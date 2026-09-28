// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package content

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuh"
)

// Component is the report component prefix: content/<tenant> for a
// Wazuh manager, content/velociraptor for the artifacts.
const Component = "content"

// ComponentName is the report component of one tenant's manager.
func ComponentName(tenant string) string { return Component + "/" + tenant }

// Manager is the part of the Wazuh manager API the rollout uses;
// *wazuh.Client implements it.
type Manager interface {
	GetFile(ctx context.Context, kind, name string) ([]byte, error)
	PutFile(ctx context.Context, kind, name string, content []byte) error
	DeleteFile(ctx context.Context, kind, name string) error
	ValidateConfiguration(ctx context.Context) error
	ReloadAnalysisd(ctx context.Context) ([]string, error)
	RegisteredLists(ctx context.Context) ([]string, error)
	GroupConfig(ctx context.Context, group string) ([]byte, error)
	CreateGroup(ctx context.Context, group string) error
	PutGroupConfig(ctx context.Context, group string, content []byte) error
}

// Target is one tenant's manager. A nil API with Err set reports that
// the manager could not be reached (no credentials, bad TLS settings).
type Target struct {
	Tenant string
	API    Manager
	Err    error
}

// RolloutWazuh brings every manager to its tenant's content. The first
// target is the canary: when its rollout fails (and is rolled back) the
// others are left alone. In dry-run mode nothing is written and every
// target is planned.
func RolloutWazuh(ctx context.Context, targets []Target, b *Bundle, store StateStore, dryRun bool) []report.Result {
	var results []report.Result
	for i, t := range targets {
		res, ok := rolloutOne(ctx, t, b.ForTenant(t.Tenant), store, dryRun)
		results = append(results, res...)
		if i == 0 && !ok && !dryRun && len(targets) > 1 {
			for _, rest := range targets[1:] {
				results = append(results, report.Result{
					Component: ComponentName(rest.Tenant), Kind: "rollout", Name: b.Version, Action: report.ActionSkip,
					Detail: fmt.Sprintf("canary %s failed; not rolled out", t.Tenant),
				})
			}
			break
		}
	}
	return results
}

// change is one planned file write.
type change struct {
	key    string
	kind   string
	name   string
	want   []byte // nil: delete
	before []byte // live content before, nil when absent
	action report.Action
}

type rollout struct {
	ctx     context.Context
	t       Target
	set     Set
	prev    *State
	dryRun  bool
	results []report.Result
}

func (r *rollout) add(kind, name string, a report.Action, detail string) {
	r.results = append(r.results, report.Result{Component: ComponentName(r.t.Tenant), Kind: kind, Name: name, Action: a, Detail: detail})
}

// isRuleset reports whether a kind is reloaded by analysisd.
func isRuleset(kind string) bool {
	return kind == KindRules || kind == KindDecoders || kind == KindLists
}

// rolloutOne rolls one manager out and reports whether it ended on the
// new content (or, in dry-run mode, whether the plan could be made).
func rolloutOne(ctx context.Context, t Target, set Set, store StateStore, dryRun bool) ([]report.Result, bool) {
	r := &rollout{ctx: ctx, t: t, set: set, dryRun: dryRun}
	if t.Err != nil {
		r.add("setup", "manager", report.ActionError, t.Err.Error())
		return r.results, false
	}
	prev, err := store.Load(ctx, stateKey(t.Tenant))
	if err != nil {
		r.add("state", stateKey(t.Tenant), report.ActionError, err.Error())
		return r.results, false
	}
	r.prev = prev

	changes, err := r.plan()
	if err != nil {
		r.add("api", "plan", report.ActionError, err.Error())
		return r.results, false
	}
	if !r.checkLists() {
		return r.results, false
	}
	for _, c := range changes {
		detail := ""
		if c.want == nil {
			detail = "no longer in the content"
		}
		r.add("wazuh-"+c.kind, c.name, c.action, detail)
	}
	if dryRun {
		r.applyGroups()
		r.summary(len(changes))
		return r.results, true
	}
	if len(changes) > 0 {
		if !r.applyRuleset(changes) {
			return r.results, false
		}
	}
	if !r.applyGroups() {
		return r.results, false
	}
	if prev == nil || prev.Hash != set.Hash || prev.Version != set.Version {
		if err := store.Save(ctx, stateKey(t.Tenant), State(set)); err != nil {
			r.add("state", stateKey(t.Tenant), report.ActionError, err.Error())
			return r.results, false
		}
	}
	r.summary(len(changes))
	return r.results, true
}

func stateKey(tenant string) string { return "wazuh-" + tenant }

func (r *rollout) summary(changes int) {
	a := report.ActionOK
	if changes > 0 {
		a = report.ActionUpdate
	}
	r.add("rollout", r.set.Version, a, fmt.Sprintf("content %s (%s), %d file changes", r.set.Version, shortHash(r.set.Hash), changes))
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// plan compares the ruleset files (not agent groups) with the manager:
// wanted files that differ, and files of the previous content that are
// no longer wanted.
func (r *rollout) plan() ([]change, error) {
	var out []change
	keys := sortedKeys(r.set.Files)
	for _, key := range keys {
		kind, name := splitKey(key)
		if !isRuleset(kind) {
			continue
		}
		want := r.set.Files[key]
		cur, err := r.t.API.GetFile(r.ctx, kind, name)
		switch {
		case errors.Is(err, wazuh.ErrNotFound):
			out = append(out, change{key: key, kind: kind, name: name, want: want, action: report.ActionCreate})
		case err != nil:
			return nil, err
		case sameText(cur, want):
		default:
			out = append(out, change{key: key, kind: kind, name: name, want: want, before: cur, action: report.ActionUpdate})
		}
	}
	if r.prev != nil {
		for _, key := range sortedKeys(r.prev.Files) {
			kind, name := splitKey(key)
			if _, still := r.set.Files[key]; still || !isRuleset(kind) {
				continue
			}
			cur, err := r.t.API.GetFile(r.ctx, kind, name)
			if errors.Is(err, wazuh.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			out = append(out, change{key: key, kind: kind, name: name, before: cur, action: report.ActionUpdate})
		}
	}
	return out, nil
}

// checkLists reports content lists the manager's <ruleset> does not
// register: their rules would be skipped, and registering a list is an
// ossec.conf change (wazuh.ruleset.extraLists) and a restart, which the
// chart does, not the reconciler.
func (r *rollout) checkLists() bool {
	var lists []string
	for key := range r.set.Files {
		if kind, name := splitKey(key); kind == KindLists {
			lists = append(lists, name)
		}
	}
	if len(lists) == 0 {
		return true
	}
	registered, err := r.t.API.RegisteredLists(r.ctx)
	if err != nil {
		r.add("api", "ruleset lists", report.ActionError, err.Error())
		return false
	}
	have := map[string]bool{}
	for _, l := range registered {
		have[l] = true
	}
	ok := true
	sort.Strings(lists)
	for _, l := range lists {
		if !have["etc/lists/"+l] {
			r.add("wazuh-lists", l, report.ActionError, "not registered in the manager's <ruleset>: add etc/lists/"+l+" to wazuh.ruleset.extraLists")
			ok = false
		}
	}
	return ok
}

// applyRuleset writes the changes, validates the configuration and
// reloads analysisd. Any failure, or a reload warning the old content
// did not have, puts the last good content back.
func (r *rollout) applyRuleset(changes []change) bool {
	before, err := r.t.API.ReloadAnalysisd(r.ctx)
	if err != nil {
		r.add("analysisd", "reload", report.ActionError, "baseline reload: "+err.Error())
		return false
	}
	var done []change
	fail := func(kind, name, why string) bool {
		r.add(kind, name, report.ActionError, why)
		r.rollback(done)
		return false
	}
	for _, c := range changes {
		done = append(done, c)
		var err error
		if c.want == nil {
			err = r.t.API.DeleteFile(r.ctx, c.kind, c.name)
		} else {
			err = r.t.API.PutFile(r.ctx, c.kind, c.name, c.want)
		}
		if err != nil {
			return fail("wazuh-"+c.kind, c.name, err.Error())
		}
	}
	if err := r.t.API.ValidateConfiguration(r.ctx); err != nil {
		return fail("validation", "configuration", err.Error())
	}
	after, err := r.t.API.ReloadAnalysisd(r.ctx)
	if err != nil {
		return fail("analysisd", "reload", err.Error())
	}
	if bad := newWarnings(before, after, r.set); len(bad) > 0 {
		return fail("analysisd", "reload", "new ruleset warnings: "+strings.Join(bad, "; "))
	}
	r.add("analysisd", "reload", report.ActionUpdate, "ruleset reloaded")
	return true
}

// listNotLoaded matches the warning for a rule whose CDB list is
// missing (7616), with the list path.
var listNotLoaded = regexp.MustCompile(`\(7616\): List '([^']+)' could not be loaded`)

// newWarnings returns the reload warnings that were not there before
// the change. A missing list the content does not ship is fine: the
// MISP export fills those lists after the rules arrive.
func newWarnings(before, after []string, set Set) []string {
	seen := map[string]bool{}
	for _, w := range before {
		seen[w] = true
	}
	var out []string
	for _, w := range after {
		if seen[w] {
			continue
		}
		if m := listNotLoaded.FindStringSubmatch(w); m != nil {
			if _, ours := set.Files[KindLists+"/"+strings.TrimPrefix(m[1], "etc/lists/")]; !ours {
				continue
			}
		}
		out = append(out, w)
	}
	return out
}

// rollback undoes applied changes: each file goes back to the last
// good content, else to what the manager had before, else away. Then
// analysisd reloads.
func (r *rollout) rollback(done []change) {
	var errs []string
	for i := len(done) - 1; i >= 0; i-- {
		c := done[i]
		restore := c.before
		if r.prev != nil {
			if lg, ok := r.prev.Files[c.key]; ok {
				restore = lg
			}
		}
		var err error
		if restore == nil {
			err = r.t.API.DeleteFile(r.ctx, c.kind, c.name)
		} else {
			err = r.t.API.PutFile(r.ctx, c.kind, c.name, restore)
		}
		if err != nil {
			errs = append(errs, c.key+": "+err.Error())
		}
	}
	if _, err := r.t.API.ReloadAnalysisd(r.ctx); err != nil {
		errs = append(errs, "reload: "+err.Error())
	}
	version := "the previous content"
	if r.prev != nil {
		version = "content " + r.prev.Version
	}
	if len(errs) > 0 {
		r.add("rollback", r.set.Version, report.ActionError, "rolling back to "+version+" failed: "+strings.Join(errs, "; "))
		return
	}
	r.add("rollback", r.set.Version, report.ActionUpdate, fmt.Sprintf("%d files back to %s", len(done), version))
}

// applyGroups creates missing agent groups and writes their agent.conf.
// Groups are never deleted: agents may still be assigned to them.
func (r *rollout) applyGroups() bool {
	ok := true
	for _, key := range sortedKeys(r.set.Files) {
		kind, group := splitKey(key)
		if kind != KindAgentGroups {
			continue
		}
		want := r.set.Files[key]
		cur, err := r.t.API.GroupConfig(r.ctx, group)
		missing := errors.Is(err, wazuh.ErrNotFound)
		switch {
		case err != nil && !missing:
			r.add("wazuh-agent-group", group, report.ActionError, err.Error())
			ok = false
			continue
		case !missing && sameXML(cur, want):
			continue
		}
		a := report.ActionUpdate
		if missing {
			a = report.ActionCreate
		}
		if r.dryRun {
			r.add("wazuh-agent-group", group, a, "")
			continue
		}
		if missing {
			if err := r.t.API.CreateGroup(r.ctx, group); err != nil {
				r.add("wazuh-agent-group", group, report.ActionError, err.Error())
				ok = false
				continue
			}
		}
		if err := r.t.API.PutGroupConfig(r.ctx, group, want); err != nil {
			r.add("wazuh-agent-group", group, report.ActionError, err.Error())
			ok = false
			continue
		}
		r.add("wazuh-agent-group", group, a, "")
	}
	if r.prev != nil {
		for _, key := range sortedKeys(r.prev.Files) {
			if kind, group := splitKey(key); kind == KindAgentGroups {
				if _, still := r.set.Files[key]; !still {
					r.add("wazuh-agent-group", group, report.ActionSkip, "no longer in the content; the group is left in place")
				}
			}
		}
	}
	return ok
}

// sameText compares file content as the API returns it (it may add or
// drop a final newline).
func sameText(a, b []byte) bool {
	return bytes.Equal(bytes.TrimRight(a, " \t\r\n"), bytes.TrimRight(b, " \t\r\n"))
}

// sameXML compares agent.conf content line by line, ignoring
// indentation and blank lines: the API re-indents it.
func sameXML(a, b []byte) bool {
	norm := func(x []byte) string {
		var lines []string
		for _, l := range strings.Split(string(x), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
		return strings.Join(lines, "\n")
	}
	return norm(a) == norm(b)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
