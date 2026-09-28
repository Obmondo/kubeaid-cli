// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name; results carry
// ComponentName(scope).
const Component = "retention"

// ComponentName is "retention/<scope>" (a tenant code or "central").
func ComponentName(scope string) string { return Component + "/" + scope }

// Report kinds.
const (
	KindPolicy  = "ism-policy"
	KindIndices = "ism-indices"
)

// ISM document keys.
const (
	keyActions     = "actions"
	keyPolicy      = "policy"
	keyName        = "name"
	keyTransitions = "transitions"
)

// ISM states.
const (
	stateHot    = "hot"
	stateWarm   = "warm"
	stateDelete = "delete"
)

// ismTemplatePriority wins over a policy template without a priority
// (0); the Wazuh indexer ships none for these indices.
const ismTemplatePriority = 100

// changePolicyChunk bounds the index names per change_policy call.
const changePolicyChunk = 50

// Policy is the desired retention policy of one indexer.
type Policy struct {
	ID            string
	RetentionDays int
	// WarmAfterDays moves indices to a read-only warm state first when
	// 0 < WarmAfterDays < RetentionDays.
	WarmAfterDays int
	IndexPatterns []string
}

// Body is the ISM policy document (the "policy" object of a PUT).
func (p Policy) Body() map[string]any {
	deleteAfter := fmt.Sprintf("%dd", p.RetentionDays)
	toDelete := []any{map[string]any{"state_name": stateDelete, "conditions": map[string]any{"min_index_age": deleteAfter}}}
	states := []any{}
	if p.WarmAfterDays > 0 && p.WarmAfterDays < p.RetentionDays {
		states = append(states,
			map[string]any{keyName: stateHot, keyActions: []any{}, keyTransitions: []any{
				map[string]any{"state_name": stateWarm, "conditions": map[string]any{"min_index_age": fmt.Sprintf("%dd", p.WarmAfterDays)}},
			}},
			map[string]any{keyName: stateWarm, keyActions: []any{map[string]any{"read_only": map[string]any{}}}, keyTransitions: toDelete},
		)
	} else {
		states = append(states, map[string]any{keyName: stateHot, keyActions: []any{}, keyTransitions: toDelete})
	}
	states = append(states, map[string]any{keyName: stateDelete, keyActions: []any{map[string]any{"delete": map[string]any{}}}, keyTransitions: []any{}})
	patterns := append([]string(nil), p.IndexPatterns...)
	sort.Strings(patterns)
	return map[string]any{
		"description":   p.description(),
		"default_state": stateHot,
		"states":        states,
		"ism_template":  []any{map[string]any{"index_patterns": patterns, "priority": ismTemplatePriority}},
	}
}

// Fingerprint identifies the policy content. OpenSearch adds fields
// (retry settings, timestamps) to a stored policy, so the stored one is
// compared through the fingerprint in its description, not field by
// field.
func (p Policy) Fingerprint() string {
	patterns := append([]string(nil), p.IndexPatterns...)
	sort.Strings(patterns)
	warm := 0
	if p.WarmAfterDays > 0 && p.WarmAfterDays < p.RetentionDays {
		warm = p.WarmAfterDays
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "v1|%d|%d|%s", p.RetentionDays, warm, strings.Join(patterns, ",")))
	return hex.EncodeToString(sum[:])[:12]
}

func (p Policy) description() string {
	return fmt.Sprintf("kubesoc retention: delete after %dd (managed by siem-reconciler, spec %s)", p.RetentionDays, p.Fingerprint())
}

// storedPolicy is GET _plugins/_ism/policies/<id>.
type storedPolicy struct {
	SeqNo       int64 `json:"_seq_no"`
	PrimaryTerm int64 `json:"_primary_term"`
	Policy      struct {
		Description string `json:"description"`
	} `json:"policy"`
}

// explained is one index of GET _plugins/_ism/explain/<pattern>.
type explained struct {
	PolicyID    *string `json:"index.plugins.index_state_management.policy_id"`
	OldPolicyID *string `json:"index.opendistro.index_state_management.policy_id"`
	SeqNo       *int64  `json:"policy_seq_no"`
}

func (e explained) policy() string {
	switch {
	case e.PolicyID != nil:
		return *e.PolicyID
	case e.OldPolicyID != nil:
		return *e.OldPolicyID
	}
	return ""
}

type retentionRun struct {
	component string
	results   []report.Result
}

func (r *retentionRun) add(kind, name string, a report.Action, detail string) {
	r.results = append(r.results, report.Result{Component: r.component, Kind: kind, Name: name, Action: a, Detail: detail})
}

// Reconcile keeps policy p on the indexer: it creates or updates the
// ISM policy (its ism_template attaches it to new indices), attaches it
// to existing indices that have no policy, and moves the indices it
// manages to the current policy version. Indices under another policy
// are reported and left alone.
func Reconcile(ctx context.Context, c *Client, component string, p Policy, dryRun bool) []report.Result {
	r := &retentionRun{component: component}
	if p.RetentionDays <= 0 {
		r.add(KindPolicy, p.ID, report.ActionSkip, "no retentionDays: retention not managed")
		return r.results
	}
	seqNo, ok := r.ensurePolicy(ctx, c, p, dryRun)
	if !ok {
		return r.results
	}
	for _, pattern := range p.IndexPatterns {
		r.attach(ctx, c, p, pattern, seqNo, dryRun)
	}
	return r.results
}

// ensurePolicy returns the current (or, in a dry run, expected) seq_no
// of the policy; -1 when it does not exist yet in a dry run.
func (r *retentionRun) ensurePolicy(ctx context.Context, c *Client, p Policy, dryRun bool) (int64, bool) {
	path := "/_plugins/_ism/policies/" + url.PathEscape(p.ID)
	detail := fmt.Sprintf("delete after %dd", p.RetentionDays)
	if p.WarmAfterDays > 0 && p.WarmAfterDays < p.RetentionDays {
		detail += fmt.Sprintf(", read-only after %dd", p.WarmAfterDays)
	}
	detail += ": " + strings.Join(p.IndexPatterns, ",")

	var cur storedPolicy
	err := c.call(ctx, http.MethodGet, path, nil, &cur)
	switch {
	case IsNotFound(err):
		if dryRun {
			r.add(KindPolicy, p.ID, report.ActionCreate, detail)
			return -1, true
		}
		var created storedPolicy
		if err := c.call(ctx, http.MethodPut, path, map[string]any{keyPolicy: p.Body()}, &created); err != nil {
			r.add(KindPolicy, p.ID, report.ActionError, err.Error())
			return 0, false
		}
		r.add(KindPolicy, p.ID, report.ActionCreate, detail)
		return created.SeqNo, true
	case err != nil:
		r.add(KindPolicy, p.ID, report.ActionError, err.Error())
		return 0, false
	}
	if strings.Contains(cur.Policy.Description, "spec "+p.Fingerprint()) {
		r.add(KindPolicy, p.ID, report.ActionOK, detail)
		return cur.SeqNo, true
	}
	if dryRun {
		r.add(KindPolicy, p.ID, report.ActionUpdate, detail)
		return -1, true
	}
	var updated storedPolicy
	q := fmt.Sprintf("%s?if_seq_no=%d&if_primary_term=%d", path, cur.SeqNo, cur.PrimaryTerm)
	if err := c.call(ctx, http.MethodPut, q, map[string]any{keyPolicy: p.Body()}, &updated); err != nil {
		r.add(KindPolicy, p.ID, report.ActionError, err.Error())
		return 0, false
	}
	r.add(KindPolicy, p.ID, report.ActionUpdate, detail)
	return updated.SeqNo, true
}

// attach puts the unmanaged indices of pattern under the policy and
// moves the ones on an older policy version (seq_no) to the current
// one. seqNo -1 (dry run, policy not written yet) marks every index of
// ours as outdated.
func (r *retentionRun) attach(ctx context.Context, c *Client, p Policy, pattern string, seqNo int64, dryRun bool) {
	idx, err := explain(ctx, c, pattern)
	if err != nil {
		r.add(KindIndices, pattern, report.ActionError, err.Error())
		return
	}
	var unmanaged, stale, foreign []string
	managed := 0
	for name, e := range idx {
		switch pol := e.policy(); {
		case pol == "":
			unmanaged = append(unmanaged, name)
		case pol != p.ID:
			foreign = append(foreign, name)
		case e.SeqNo != nil && (seqNo < 0 || *e.SeqNo != seqNo):
			stale = append(stale, name)
		default:
			managed++
		}
	}
	sort.Strings(stale)
	if len(unmanaged) > 0 {
		r.addPolicy(ctx, c, p, pattern, len(unmanaged), dryRun)
	}
	if len(stale) > 0 {
		r.changePolicy(ctx, c, p, pattern, stale, dryRun)
	}
	if len(unmanaged) == 0 && len(stale) == 0 {
		detail := fmt.Sprintf("%d indices managed", managed)
		if len(foreign) > 0 {
			// Another policy's indices are the operator's decision.
			detail += fmt.Sprintf(", %d under another policy left alone", len(foreign))
		}
		r.add(KindIndices, pattern, report.ActionOK, detail)
	}
}

func (r *retentionRun) addPolicy(ctx context.Context, c *Client, p Policy, pattern string, n int, dryRun bool) {
	detail := fmt.Sprintf("attach policy to %d existing indices", n)
	if dryRun {
		r.add(KindIndices, pattern, report.ActionCreate, detail)
		return
	}
	var out struct {
		Updated       int  `json:"updated_indices"`
		Failures      bool `json:"failures"`
		FailedIndices []struct {
			Name   string `json:"index_name"`
			Reason string `json:"reason"`
		} `json:"failed_indices"`
	}
	if err := c.call(ctx, http.MethodPost, "/_plugins/_ism/add/"+pattern, map[string]any{"policy_id": p.ID}, &out); err != nil {
		r.add(KindIndices, pattern, report.ActionError, err.Error())
		return
	}
	// An index that got a policy between explain and add is reported as
	// failed ("already has a policy"); that is not an error.
	var failed []string
	for _, f := range out.FailedIndices {
		if !strings.Contains(strings.ToLower(f.Reason), "already") {
			failed = append(failed, f.Name+": "+f.Reason)
		}
	}
	if len(failed) > 0 {
		r.add(KindIndices, pattern, report.ActionError, fmt.Sprintf("attached %d, failed %d: %s", out.Updated, len(failed), strings.Join(failed, "; ")))
		return
	}
	r.add(KindIndices, pattern, report.ActionCreate, fmt.Sprintf("attached policy to %d existing indices", out.Updated))
}

func (r *retentionRun) changePolicy(ctx context.Context, c *Client, p Policy, pattern string, stale []string, dryRun bool) {
	detail := fmt.Sprintf("move %d indices to the current policy version", len(stale))
	if dryRun {
		r.add(KindIndices, pattern, report.ActionUpdate, detail)
		return
	}
	for start := 0; start < len(stale); start += changePolicyChunk {
		chunk := stale[start:min(start+changePolicyChunk, len(stale))]
		var out struct {
			Failures      bool `json:"failures"`
			FailedIndices []struct {
				Name   string `json:"index_name"`
				Reason string `json:"reason"`
			} `json:"failed_indices"`
		}
		path := "/_plugins/_ism/change_policy/" + strings.Join(chunk, ",")
		if err := c.call(ctx, http.MethodPost, path, map[string]any{"policy_id": p.ID}, &out); err != nil {
			r.add(KindIndices, pattern, report.ActionError, err.Error())
			return
		}
		if out.Failures && len(out.FailedIndices) > 0 {
			f := out.FailedIndices[0]
			r.add(KindIndices, pattern, report.ActionError, fmt.Sprintf("change_policy failed for %d indices, e.g. %s: %s", len(out.FailedIndices), f.Name, f.Reason))
			return
		}
	}
	r.add(KindIndices, pattern, report.ActionUpdate, detail)
}

// Patterns (validated in the config) and index names (from the
// indexer) hold only [a-z0-9._*-], so they go into paths unescaped:
// OpenSearch reads the comma-separated lists and wildcards literally.

// explain returns the ISM state of every index matching pattern. A
// pattern matching no index yields an empty map.
func explain(ctx context.Context, c *Client, pattern string) (map[string]explained, error) {
	var raw map[string]json.RawMessage
	if err := c.call(ctx, http.MethodGet, "/_plugins/_ism/explain/"+pattern, nil, &raw); err != nil {
		if IsNotFound(err) {
			return map[string]explained{}, nil
		}
		return nil, err
	}
	out := make(map[string]explained, len(raw))
	for name, v := range raw {
		if len(v) == 0 || v[0] != '{' {
			continue // total_managed_indices
		}
		var e explained
		if err := json.Unmarshal(v, &e); err != nil {
			return nil, fmt.Errorf("decoding ISM explain of %s: %w", name, err)
		}
		out[name] = e
	}
	return out, nil
}
