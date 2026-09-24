// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package velociraptor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Component is the report component name.
const Component = "velociraptor"

// VQL used by the reconciler. Inputs come in as environment
// variables (OrgName, Artifact, Params).
const (
	vqlListOrgs      = "SELECT OrgId, Name FROM orgs()"
	vqlCreateOrg     = "SELECT org_create(name=OrgName) AS Org FROM scope()"
	vqlGetMonitoring = "SELECT get_server_monitoring() AS State FROM scope()"
	vqlAddMonitoring = "SELECT add_server_monitoring(artifact=Artifact, parameters=parse_json(data=Params)) AS Result FROM scope()"
)

// Querier runs VQL; *Client implements it.
type Querier interface {
	Query(ctx context.Context, vql string, env map[string]string) ([]map[string]any, []string, error)
}

// MonitoredArtifact is a server event artifact that must be running
// with (at least) the given parameters.
type MonitoredArtifact struct {
	Artifact   string
	Parameters map[string]string
}

// Spec is the desired Velociraptor state.
type Spec struct {
	// Orgs are org names (one per tenant). Orgs are never deleted.
	Orgs []string
	// Monitoring entries are added or have their listed parameters
	// set; other artifacts in the table are left alone.
	Monitoring []MonitoredArtifact
}

// Reconcile brings orgs and the server monitoring table in line.
func Reconcile(ctx context.Context, q Querier, spec Spec, dryRun bool) []report.Result {
	var results []report.Result
	add := func(kind, name string, a report.Action, detail string) {
		results = append(results, report.Result{Component: Component, Kind: kind, Name: name, Action: a, Detail: detail})
	}

	if len(spec.Orgs) > 0 {
		rows, _, err := q.Query(ctx, vqlListOrgs, nil)
		if err != nil {
			add("api", "orgs", report.ActionError, err.Error())
		} else {
			have := map[string]int{}
			for _, r := range rows {
				have[str(r["Name"])]++
			}
			for _, name := range spec.Orgs {
				switch {
				case have[name] > 1:
					add("org", name, report.ActionError, "more than one org with this name; routing by name would be ambiguous")
				case have[name] == 1:
					add("org", name, report.ActionOK, "")
				case dryRun:
					add("org", name, report.ActionCreate, "")
				default:
					a, detail := runWrite(ctx, q, vqlCreateOrg, map[string]string{"OrgName": name}, "Org")
					add("org", name, a, detail)
				}
			}
		}
	}

	if len(spec.Monitoring) == 0 {
		return results
	}
	current, err := monitoringState(ctx, q)
	if err != nil {
		add("api", "server-monitoring", report.ActionError, err.Error())
		return results
	}
	for _, want := range spec.Monitoring {
		params, present := current[want.Artifact]
		drift := driftedParams(params, want.Parameters)
		switch {
		case present && len(drift) == 0:
			add("server-monitoring", want.Artifact, report.ActionOK, "")
			continue
		case dryRun && !present:
			add("server-monitoring", want.Artifact, report.ActionCreate, "")
			continue
		case dryRun:
			add("server-monitoring", want.Artifact, report.ActionUpdate, "parameters "+strings.Join(drift, ","))
			continue
		}
		// add_server_monitoring replaces the artifact's whole spec, so
		// keep the parameters the reconciler does not manage.
		merged := map[string]string{}
		for k, v := range params {
			merged[k] = v
		}
		for k, v := range want.Parameters {
			merged[k] = v
		}
		raw, err := json.Marshal(merged)
		if err != nil {
			add("server-monitoring", want.Artifact, report.ActionError, err.Error())
			continue
		}
		a, detail := runWrite(ctx, q, vqlAddMonitoring, map[string]string{"Artifact": want.Artifact, "Params": string(raw)}, "Result")
		if a != report.ActionError && present {
			a, detail = report.ActionUpdate, "parameters "+strings.Join(drift, ",")
		}
		add("server-monitoring", want.Artifact, a, detail)
	}
	return results
}

// runWrite runs a VQL function call whose result column is null on
// failure (Velociraptor functions log the reason instead of failing).
func runWrite(ctx context.Context, q Querier, vql string, env map[string]string, column string) (report.Action, string) {
	rows, logs, err := q.Query(ctx, vql, env)
	if err != nil {
		return report.ActionError, err.Error()
	}
	if len(rows) == 0 || rows[0][column] == nil {
		return report.ActionError, "server refused: " + lastLogs(logs)
	}
	return report.ActionCreate, ""
}

// monitoringState returns artifact -> parameters of the server
// monitoring table.
func monitoringState(ctx context.Context, q Querier) (map[string]map[string]string, error) {
	rows, logs, err := q.Query(ctx, vqlGetMonitoring, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0]["State"] == nil {
		return nil, fmt.Errorf("reading server monitoring state: %s", lastLogs(logs))
	}
	state, ok := rows[0]["State"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected server monitoring state %T", rows[0]["State"])
	}
	out := map[string]map[string]string{}
	for _, a := range list(field(state, "artifacts")) {
		out[str(a)] = map[string]string{}
	}
	for _, s := range list(field(state, "specs")) {
		spec, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name := str(field(spec, "artifact"))
		params := out[name]
		if params == nil {
			// A spec without a matching artifacts entry is not running.
			continue
		}
		p, _ := field(spec, "parameters").(map[string]any)
		for _, e := range list(field(p, "env")) {
			kv, ok := e.(map[string]any)
			if ok {
				params[str(field(kv, "key"))] = str(field(kv, "value"))
			}
		}
	}
	return out, nil
}

// driftedParams lists the wanted parameters whose value differs.
// A parameter absent from the spec runs with the artifact default;
// it counts as drift because the default may differ.
func driftedParams(have, want map[string]string) []string {
	var out []string
	for k, v := range want {
		if cur, ok := have[k]; !ok || cur != v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// field reads a key case-insensitively (protobuf JSON may use either
// the proto name or the Go name).
func field(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func lastLogs(logs []string) string {
	if len(logs) == 0 {
		return "no log output"
	}
	if len(logs) > 3 {
		logs = logs[len(logs)-3:]
	}
	return strings.Join(logs, " | ")
}
