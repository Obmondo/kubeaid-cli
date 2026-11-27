// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuhcentral

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name.
const Component = "wazuhcentral"

const (
	remotePrefix = "cluster.remote."
	seedsSuffix  = ".seeds"
	proxySuffix  = ".proxy_address"
	kindRemote   = "remote"
)

// Remote is one cross-cluster search connection.
type Remote struct {
	Alias string
	Seeds []string
}

// Spec is the desired central indexer state.
type Spec struct {
	Remotes []Remote
}

// Reconcile ensures the persistent cluster.remote.<alias>.seeds
// setting of every remote in spec. Remotes the indexer has but spec
// does not list are reported as skipped and left in place.
func Reconcile(ctx context.Context, c *Client, spec Spec, dryRun bool) []report.Result {
	var results []report.Result
	add := func(kind, name string, a report.Action, detail string) {
		results = append(results, report.Result{Component: Component, Kind: kind, Name: name, Action: a, Detail: detail})
	}
	settings, err := c.persistentSettings(ctx)
	if err != nil {
		add("api", "cluster-settings", report.ActionError, err.Error())
		return results
	}
	have := remotes(settings)

	wanted := map[string]bool{}
	for _, r := range spec.Remotes {
		wanted[r.Alias] = true
		cur, ok := have[r.Alias]
		action := report.ActionCreate
		switch {
		case ok && sameSet(cur, r.Seeds):
			add(kindRemote, r.Alias, report.ActionOK, "")
			continue
		case ok:
			action = report.ActionUpdate
		}
		detail := "seeds " + strings.Join(r.Seeds, ",")
		if !dryRun {
			err := c.putPersistent(ctx, map[string]any{remotePrefix + r.Alias + seedsSuffix: r.Seeds})
			if err != nil {
				add(kindRemote, r.Alias, report.ActionError, err.Error())
				continue
			}
		}
		add(kindRemote, r.Alias, action, detail)
	}

	extra := make([]string, 0, len(have))
	for alias := range have {
		if !wanted[alias] {
			extra = append(extra, alias)
		}
	}
	sort.Strings(extra)
	for _, alias := range extra {
		add(kindRemote, alias, report.ActionSkip, "extra: not in config, left in place")
	}
	return results
}

// remotes extracts the configured remote clusters from flat persistent
// settings: alias -> seeds. A proxy-mode remote has no seeds but is
// still a remote.
func remotes(settings map[string]json.RawMessage) map[string][]string {
	out := map[string][]string{}
	for key, raw := range settings {
		if !strings.HasPrefix(key, remotePrefix) {
			continue
		}
		rest := strings.TrimPrefix(key, remotePrefix)
		var alias string
		switch {
		case strings.HasSuffix(rest, seedsSuffix):
			alias = strings.TrimSuffix(rest, seedsSuffix)
		case strings.HasSuffix(rest, proxySuffix):
			alias = strings.TrimSuffix(rest, proxySuffix)
		default:
			continue
		}
		if alias == "" || strings.Contains(alias, ".") {
			continue
		}
		seeds := out[alias]
		if strings.HasSuffix(rest, seedsSuffix) {
			seeds = stringList(raw)
		}
		out[alias] = seeds
	}
	return out
}

// stringList decodes a setting that is either a JSON list or a
// comma-separated string.
func stringList(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func sameSet(a, b []string) bool {
	x := slices.Clone(a)
	y := slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}
