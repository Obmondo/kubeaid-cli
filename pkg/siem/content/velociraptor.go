// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// ComponentVelociraptor is the report component of the artifacts.
const ComponentVelociraptor = Component + "/velociraptor"

// velociraptorStateKey is the state Secret key of the artifacts.
const velociraptorStateKey = "velociraptor"

// VQL of the artifact rollout. Inputs come in as environment variables.
const (
	vqlListArtifacts  = "SELECT name, raw, built_in FROM artifact_definitions(names=parse_json_array(data=Names))"
	vqlSetArtifact    = "SELECT artifact_set(definition=Definition) AS Result FROM scope()"
	vqlDeleteArtifact = "SELECT artifact_delete(name=ArtifactName) AS Result FROM scope()"
)

// Querier runs VQL; *velociraptor.Client implements it.
type Querier interface {
	Query(ctx context.Context, vql string, env map[string]string) ([]map[string]any, []string, error)
}

type liveArtifact struct {
	raw     string
	builtIn bool
}

// RolloutArtifacts sets every artifact of the bundle on the server with
// artifact_set, which stores it in the datastore: live at once, and
// kept across restarts, so the server needs neither the artifacts
// ConfigMap mount nor a restart. Artifacts the content shipped before
// and no longer does are deleted. Artifacts loaded from the server's
// definitions directory (the chart's customArtifacts mount) are built
// in and cannot be replaced; they are reported.
func RolloutArtifacts(ctx context.Context, q Querier, b *Bundle, store StateStore, dryRun bool) []report.Result {
	var results []report.Result
	add := func(kind, name string, a report.Action, detail string) {
		results = append(results, report.Result{Component: ComponentVelociraptor, Kind: kind, Name: name, Action: a, Detail: detail})
	}
	prev, err := store.Load(ctx, velociraptorStateKey)
	if err != nil {
		add("state", velociraptorStateKey, report.ActionError, err.Error())
		return results
	}
	names := sortedKeys(b.Artifacts)
	var gone []string
	if prev != nil {
		for _, n := range sortedKeys(prev.Files) {
			if _, ok := b.Artifacts[n]; !ok {
				gone = append(gone, n)
			}
		}
	}
	live, err := listArtifacts(ctx, q, append(append([]string(nil), names...), gone...))
	if err != nil {
		add("api", "artifacts", report.ActionError, err.Error())
		return results
	}

	files := make(map[string][]byte, len(names))
	for _, n := range names {
		files[n] = b.Artifacts[n].Raw
	}
	changes, failed := setArtifacts(ctx, q, b, names, live, dryRun, add)
	dc, df := deleteArtifacts(ctx, q, gone, live, dryRun, add)
	changes += dc
	failed = failed || df
	st := State{Version: b.Version, Hash: hashFiles(files), Files: files}
	if !dryRun && !failed && (prev == nil || prev.Hash != st.Hash || prev.Version != st.Version) {
		if err := store.Save(ctx, velociraptorStateKey, st); err != nil {
			add("state", velociraptorStateKey, report.ActionError, err.Error())
		}
	}
	a := report.ActionOK
	if changes > 0 {
		a = report.ActionUpdate
	}
	add("rollout", b.Version, a, fmt.Sprintf("content %s, %d artifacts, %d changes", b.Version, len(names), changes))
	return results
}

type addFunc func(kind, name string, a report.Action, detail string)

// setArtifacts sets the artifacts that differ from the server's.
func setArtifacts(ctx context.Context, q Querier, b *Bundle, names []string, live map[string]liveArtifact, dryRun bool, add addFunc) (int, bool) {
	changes, failed := 0, false
	for _, n := range names {
		want := b.Artifacts[n]
		cur, present := live[n]
		switch {
		case present && cur.builtIn:
			add("artifact", n, report.ActionError, "loaded from the server's definitions directory, so artifact_set cannot replace it: turn velociraptor.velociraptor.customArtifacts off")
			failed = true
			continue
		case present && sameText([]byte(cur.raw), want.Raw):
			continue
		}
		a := report.ActionCreate
		if present {
			a = report.ActionUpdate
		}
		changes++
		if dryRun {
			add("artifact", n, a, "")
			continue
		}
		if detail, err := setArtifact(ctx, q, n, want.Raw); err != nil {
			add("artifact", n, report.ActionError, detail)
			failed = true
			continue
		}
		add("artifact", n, a, "")
	}
	return changes, failed
}

// deleteArtifacts deletes artifacts the previous content set and this one
// does not ship.
func deleteArtifacts(ctx context.Context, q Querier, gone []string, live map[string]liveArtifact, dryRun bool, add addFunc) (int, bool) {
	changes, failed := 0, false
	for _, n := range gone {
		if _, present := live[n]; !present {
			continue
		}
		changes++
		if dryRun {
			add("artifact", n, report.ActionUpdate, "no longer in the content; would be deleted")
			continue
		}
		if _, logs, err := q.Query(ctx, vqlDeleteArtifact, map[string]string{"ArtifactName": n}); err != nil {
			add("artifact", n, report.ActionError, "deleting: "+err.Error()+logLine(logs))
			failed = true
			continue
		}
		add("artifact", n, report.ActionUpdate, "deleted: no longer in the content")
	}
	return changes, failed
}

// listArtifacts returns the named artifacts the server has.
func listArtifacts(ctx context.Context, q Querier, names []string) (map[string]liveArtifact, error) {
	out := map[string]liveArtifact{}
	if len(names) == 0 {
		return out, nil
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return nil, err
	}
	rows, _, err := q.Query(ctx, vqlListArtifacts, map[string]string{"Names": string(raw)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		name, _ := r["name"].(string)
		rawDef, _ := r["raw"].(string)
		builtIn, _ := r["built_in"].(bool)
		if name != "" {
			out[name] = liveArtifact{raw: rawDef, builtIn: builtIn}
		}
	}
	return out, nil
}

// setArtifact runs artifact_set and reads the definition back: the
// function returns nothing useful and reports a bad definition only in
// the query log.
func setArtifact(ctx context.Context, q Querier, name string, def []byte) (string, error) {
	_, logs, err := q.Query(ctx, vqlSetArtifact, map[string]string{"Definition": string(def)})
	if err != nil {
		return err.Error() + logLine(logs), err
	}
	live, err := listArtifacts(ctx, q, []string{name})
	if err != nil {
		return err.Error(), err
	}
	if cur, ok := live[name]; !ok || !sameText([]byte(cur.raw), def) {
		err := fmt.Errorf("artifact_set did not take")
		return err.Error() + logLine(logs), err
	}
	return "", nil
}

func logLine(logs []string) string {
	if len(logs) == 0 {
		return ""
	}
	return ": " + strings.Join(logs, "; ")
}
