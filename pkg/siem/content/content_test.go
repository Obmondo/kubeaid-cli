// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package content

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuh"
)

const (
	ruleFile   = "rules/kubesoc-ioc.xml"
	ruleV1     = "<group name=\"kubesoc,\">\n  <rule id=\"100100\" level=\"5\">\n    <if_sid>5716</if_sid>\n    <description>v1</description>\n  </rule>\n</group>\n"
	ruleV2     = "<group name=\"kubesoc,\">\n  <rule id=\"100100\" level=\"7\">\n    <if_sid>5716</if_sid>\n    <description>v2</description>\n  </rule>\n</group>\n"
	badRule    = "<group name=\"kubesoc,\">\n  <rule id=\"100101\" level=\"5\">\n    <if_sid>999999</if_sid>\n    <description>broken parent</description>\n  </rule>\n</group>\n"
	agentConf  = "<agent_config>\n  <labels>\n    <label key=\"kubesoc.tenant\">t</label>\n  </labels>\n</agent_config>\n"
	artifactV1 = "name: Custom.Kubesoc.Test\nsources:\n  - query: SELECT * FROM info()\n"
	stateNS    = "security-operations"
	groupKey   = "agent-groups/linux"
	decoderKey = "decoders/kubesoc-d.xml"
	// mountedArtifact is loaded from the server's definitions directory.
	mountedArtifact = "Custom.Old.Mounted"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

func TestLoadChartDirAndMount(t *testing.T) {
	chart := t.TempDir()
	write(t, chart, "VERSION", "2026.09.1\n")
	write(t, chart, "wazuh/rules/kubesoc-ioc.xml", ruleV1)
	write(t, chart, "wazuh/rules/README.md", "docs")
	write(t, chart, "wazuh/decoders/kubesoc-d.xml", "<decoder name=\"x\"/>")
	write(t, chart, "wazuh/lists/kubesoc-bad-agents", "curl:\n")
	write(t, chart, "wazuh/agent-groups/linux/agent.conf", agentConf)
	write(t, chart, "velociraptor/artifacts/Custom.Kubesoc.Test.yaml", artifactV1)
	write(t, chart, "tests/100100/input.log", "not content")
	write(t, chart, "tenants/001/wazuh/rules/kubesoc-001.xml", ruleV2)
	write(t, chart, "manifest.json", `{"optional":["wazuh/lists/kubesoc-bad-agents"],`+
		`"tenants":{"001":{"include":["wazuh/lists/kubesoc-bad-agents"],"exclude":["wazuh/agent-groups/linux/agent.conf"]}}}`)
	core := t.TempDir()
	write(t, core, "Custom.Server.Core.yaml", "name: Custom.Server.Core\n")
	write(t, core, "..data/ignored.yaml", "name: Ignored\n")

	b, err := Load(chart, []string{core})
	require.NoError(t, err)
	assert.Equal(t, "2026.09.1", b.Version)
	assert.ElementsMatch(t, []string{ruleFile, decoderKey, "lists/kubesoc-bad-agents", groupKey}, sortedKeys(b.Wazuh))
	assert.ElementsMatch(t, []string{"Custom.Kubesoc.Test", "Custom.Server.Core"}, sortedKeys(b.Artifacts))

	t001 := b.ForTenant("001")
	assert.ElementsMatch(t, []string{ruleFile, decoderKey, "lists/kubesoc-bad-agents", "rules/kubesoc-001.xml"}, sortedKeys(t001.Files))
	t002 := b.ForTenant("002")
	assert.ElementsMatch(t, []string{ruleFile, decoderKey, groupKey}, sortedKeys(t002.Files))
	assert.NotEqual(t, t001.Hash, t002.Hash)

	// The ConfigMap mount: flat names, "__" for "/".
	mount := t.TempDir()
	write(t, mount, "VERSION", "2026.09.1")
	write(t, mount, "wazuh__rules__kubesoc-ioc.xml", ruleV1)
	write(t, mount, "wazuh__agent-groups__linux__agent.conf", agentConf)
	m, err := Load(mount, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{ruleFile, groupKey}, sortedKeys(m.Wazuh))
}

func TestLoadRejectsBadNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "VERSION", "1")
	write(t, dir, "wazuh/rules/local_rules.xml", ruleV1)
	write(t, dir, "wazuh/lists/bad name", "x:\n")
	write(t, dir, "velociraptor/artifacts/a.yaml", artifactV1)
	write(t, dir, "velociraptor/artifacts/b.yaml", artifactV1)
	_, err := Load(dir, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local_rules.xml is written by the wazuh chart")
	assert.Contains(t, err.Error(), "list name")
	assert.Contains(t, err.Error(), "defined in both")

	_, err = Load(t.TempDir(), nil)
	require.ErrorContains(t, err, "no VERSION file")
}

// fakeManager is an in-memory Wazuh manager: files under etc/, agent
// groups, and a reload that warns about rules whose if_sid is unknown
// and lists that are missing, as analysisd does.
type fakeManager struct {
	files      map[string]string
	groups     map[string]string
	registered []string
	reloads    int
	writes     int
	invalid    bool
}

func newFakeManager() *fakeManager {
	return &fakeManager{files: map[string]string{}, groups: map[string]string{}, registered: []string{"etc/lists/audit-keys", "etc/lists/misp-malicious-ip"}}
}

func (m *fakeManager) GetFile(_ context.Context, kind, name string) ([]byte, error) {
	v, ok := m.files[kind+"/"+name]
	if !ok {
		return nil, wazuh.ErrNotFound
	}
	return []byte(v + "\n"), nil // the API may add a newline
}

func (m *fakeManager) PutFile(_ context.Context, kind, name string, content []byte) error {
	m.writes++
	m.files[kind+"/"+name] = string(content)
	return nil
}

func (m *fakeManager) DeleteFile(_ context.Context, kind, name string) error {
	m.writes++
	delete(m.files, kind+"/"+name)
	return nil
}

func (m *fakeManager) ValidateConfiguration(context.Context) error {
	if m.invalid {
		return assert.AnError
	}
	return nil
}

func (m *fakeManager) ReloadAnalysisd(context.Context) ([]string, error) {
	m.reloads++
	var w []string
	for k, v := range m.files {
		if strings.HasPrefix(k, "rules/") && strings.Contains(v, "999999") {
			w = append(w, "master: (7617): Signature ID '999999' was not found in "+k)
		}
		if strings.HasPrefix(k, "rules/") && strings.Contains(v, "etc/lists/misp-malicious-ip") {
			if _, ok := m.files["lists/misp-malicious-ip"]; !ok {
				w = append(w, "master: (7616): List 'etc/lists/misp-malicious-ip' could not be loaded. Rule '99903' will be ignored.")
			}
		}
	}
	return w, nil
}

func (m *fakeManager) RegisteredLists(context.Context) ([]string, error) { return m.registered, nil }

func (m *fakeManager) GroupConfig(_ context.Context, g string) ([]byte, error) {
	v, ok := m.groups[g]
	if !ok {
		return nil, wazuh.ErrNotFound
	}
	return []byte(v), nil
}

func (m *fakeManager) CreateGroup(_ context.Context, g string) error {
	m.groups[g] = ""
	return nil
}

func (m *fakeManager) PutGroupConfig(_ context.Context, g string, content []byte) error {
	m.writes++
	// Re-indented, as the API stores it.
	m.groups[g] = "  " + strings.ReplaceAll(string(content), "\n", "\n  ")
	return nil
}

func bundle(version string, files map[string]string) *Bundle {
	b := &Bundle{Version: version, Wazuh: map[string][]byte{}, Overlays: map[string]map[string][]byte{}, Artifacts: map[string]Artifact{}}
	for k, v := range files {
		b.Wazuh[k] = []byte(v)
	}
	return b
}

func newStore() SecretStore {
	return SecretStore{Kube: fake.NewClientset(), Namespace: stateNS, Prefix: "kubesoc-content-"}
}

func actions(results []report.Result) map[string]report.Action {
	out := map[string]report.Action{}
	for _, r := range results {
		out[r.Component+" "+r.Kind+" "+r.Name] = r.Action
	}
	return out
}

func errorsOf(results []report.Result) []string {
	var out []string
	for _, r := range results {
		if r.Action == report.ActionError {
			out = append(out, r.Kind+" "+r.Name+": "+r.Detail)
		}
	}
	return out
}

func TestRolloutCreatesThenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	m := newFakeManager()
	store := newStore()
	b := bundle("1", map[string]string{ruleFile: ruleV1, groupKey: agentConf})
	targets := []Target{{Tenant: "001", API: m}}

	res := RolloutWazuh(ctx, targets, b, store, false)
	require.Empty(t, errorsOf(res))
	a := actions(res)
	assert.Equal(t, report.ActionCreate, a["content/001 wazuh-rules kubesoc-ioc.xml"])
	assert.Equal(t, report.ActionUpdate, a["content/001 analysisd reload"])
	assert.Equal(t, report.ActionCreate, a["content/001 wazuh-agent-group linux"])
	assert.Equal(t, ruleV1, m.files[ruleFile])

	st, err := store.Load(ctx, "wazuh-001")
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.Equal(t, "1", st.Version)
	sec, err := store.Kube.CoreV1().Secrets(stateNS).Get(ctx, "kubesoc-content-wazuh-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "1", sec.Labels[versionLabel])

	writes, reloads := m.writes, m.reloads
	res = RolloutWazuh(ctx, targets, b, store, false)
	require.Empty(t, errorsOf(res))
	assert.Equal(t, writes, m.writes, "second run writes nothing")
	assert.Equal(t, reloads, m.reloads, "second run does not reload")
	assert.Equal(t, report.ActionOK, actions(res)["content/001 rollout 1"])
}

func TestRolloutUpdateRemoveAndDryRun(t *testing.T) {
	ctx := context.Background()
	m := newFakeManager()
	store := newStore()
	targets := []Target{{Tenant: "001", API: m}}
	extra := "rules/kubesoc-extra.xml"
	res := RolloutWazuh(ctx, targets, bundle("1", map[string]string{ruleFile: ruleV1, extra: ruleV1}), store, false)
	require.Empty(t, errorsOf(res))

	v2 := bundle("2", map[string]string{ruleFile: ruleV2})
	writes := m.writes
	res = RolloutWazuh(ctx, targets, v2, store, true)
	require.Empty(t, errorsOf(res))
	assert.Equal(t, writes, m.writes, "dry run writes nothing")
	assert.Equal(t, report.ActionUpdate, actions(res)["content/001 wazuh-rules kubesoc-ioc.xml"])
	assert.Equal(t, report.ActionUpdate, actions(res)["content/001 wazuh-rules kubesoc-extra.xml"])

	res = RolloutWazuh(ctx, targets, v2, store, false)
	require.Empty(t, errorsOf(res))
	assert.Equal(t, ruleV2, m.files[ruleFile])
	_, stillThere := m.files[extra]
	assert.False(t, stillThere, "a file the previous content owned is deleted")
}

func TestCanaryFailureRollsBackAndStops(t *testing.T) {
	ctx := context.Background()
	canary, other := newFakeManager(), newFakeManager()
	store := newStore()
	targets := []Target{{Tenant: "001", API: canary}, {Tenant: "002", API: other}}
	res := RolloutWazuh(ctx, targets, bundle("1", map[string]string{ruleFile: ruleV1}), store, false)
	require.Empty(t, errorsOf(res))

	bad := bundle("2", map[string]string{ruleFile: badRule})
	res = RolloutWazuh(ctx, targets, bad, store, false)
	errs := errorsOf(res)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "new ruleset warnings")
	a := actions(res)
	assert.Equal(t, report.ActionUpdate, a["content/001 rollback 2"])
	assert.Equal(t, report.ActionSkip, a["content/002 rollout 2"])
	assert.Equal(t, ruleV1, canary.files[ruleFile], "canary back on the last good content")
	assert.Equal(t, ruleV1, other.files[ruleFile], "the others were not touched")
	st, err := store.Load(ctx, "wazuh-001")
	require.NoError(t, err)
	assert.Equal(t, "1", st.Version, "state keeps the last good version")
}

func TestFirstRolloutFailureRemovesNewFiles(t *testing.T) {
	ctx := context.Background()
	m := newFakeManager()
	m.invalid = true
	res := RolloutWazuh(ctx, []Target{{Tenant: "001", API: m}}, bundle("1", map[string]string{ruleFile: ruleV1}), newStore(), false)
	require.NotEmpty(t, errorsOf(res))
	assert.Empty(t, m.files, "a new file is removed again")
}

func TestDynamicListWarningIsTolerated(t *testing.T) {
	ctx := context.Background()
	m := newFakeManager()
	rule := "<group name=\"x,\">\n  <rule id=\"99903\" level=\"14\">\n    <list field=\"srcip\" lookup=\"match_key\">etc/lists/misp-malicious-ip</list>\n  </rule>\n</group>\n"
	res := RolloutWazuh(ctx, []Target{{Tenant: "001", API: m}}, bundle("1", map[string]string{ruleFile: rule}), newStore(), false)
	assert.Empty(t, errorsOf(res), "the MISP export fills that list later")
}

func TestUnregisteredListIsAnError(t *testing.T) {
	ctx := context.Background()
	m := newFakeManager()
	res := RolloutWazuh(ctx, []Target{{Tenant: "001", API: m}}, bundle("1", map[string]string{"lists/kubesoc-new": "a:\n", ruleFile: ruleV1}), newStore(), false)
	errs := errorsOf(res)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0], "wazuh.ruleset.extraLists")
	assert.Zero(t, m.writes)
}

func TestTargetErrorIsReported(t *testing.T) {
	res := RolloutWazuh(context.Background(), []Target{{Tenant: "001", Err: assert.AnError}}, bundle("1", nil), newStore(), false)
	require.Len(t, errorsOf(res), 1)
}

// fakeVelo is a Velociraptor server's artifact repository.
type fakeVelo struct {
	defs    map[string]string
	builtIn map[string]bool
	sets    int
}

func (f *fakeVelo) Query(_ context.Context, vql string, env map[string]string) ([]map[string]any, []string, error) {
	switch vql {
	case vqlListArtifacts:
		var names []string
		_ = json.Unmarshal([]byte(env["Names"]), &names)
		var rows []map[string]any
		for _, n := range names {
			if d, ok := f.defs[n]; ok {
				rows = append(rows, map[string]any{"name": n, "raw": d, "built_in": f.builtIn[n]})
			}
		}
		return rows, nil, nil
	case vqlSetArtifact:
		f.sets++
		def := env["Definition"]
		name := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(def, "\n", 2)[0], "name:"))
		if strings.Contains(def, "nosuchplugin") {
			return []map[string]any{{"Result": nil}}, []string{"artifact_set: unknown plugin"}, nil
		}
		f.defs[name] = def
		return []map[string]any{{"Result": nil}}, nil, nil
	case vqlDeleteArtifact:
		delete(f.defs, env["ArtifactName"])
		return nil, nil, nil
	}
	return nil, nil, assert.AnError
}

func TestRolloutArtifacts(t *testing.T) {
	ctx := context.Background()
	v := &fakeVelo{defs: map[string]string{mountedArtifact: "name: Custom.Old.Mounted\n"}, builtIn: map[string]bool{mountedArtifact: true}}
	store := newStore()
	b := bundle("1", nil)
	b.Artifacts["Custom.Kubesoc.Test"] = Artifact{Name: "Custom.Kubesoc.Test", Raw: []byte(artifactV1)}
	b.Artifacts["Custom.Kubesoc.Gone"] = Artifact{Name: "Custom.Kubesoc.Gone", Raw: []byte("name: Custom.Kubesoc.Gone\n")}

	res := RolloutArtifacts(ctx, v, b, store, true)
	require.Empty(t, errorsOf(res))
	assert.Zero(t, v.sets, "dry run sets nothing")

	res = RolloutArtifacts(ctx, v, b, store, false)
	require.Empty(t, errorsOf(res))
	assert.Equal(t, 2, v.sets)
	res = RolloutArtifacts(ctx, v, b, store, false)
	assert.Equal(t, 2, v.sets, "unchanged artifacts are not set again")
	assert.Equal(t, report.ActionOK, actions(res)["content/velociraptor rollout 1"])

	delete(b.Artifacts, "Custom.Kubesoc.Gone")
	b.Artifacts[mountedArtifact] = Artifact{Name: mountedArtifact, Raw: []byte("name: Custom.Old.Mounted\n# new\n")}
	b.Artifacts["Custom.Kubesoc.Bad"] = Artifact{Name: "Custom.Kubesoc.Bad", Raw: []byte("name: Custom.Kubesoc.Bad\nsources:\n  - query: SELECT * FROM nosuchplugin()\n")}
	res = RolloutArtifacts(ctx, v, b, store, false)
	errs := errorsOf(res)
	require.Len(t, errs, 2)
	assert.Contains(t, strings.Join(errs, "\n"), "customArtifacts off")
	assert.Contains(t, strings.Join(errs, "\n"), "unknown plugin")
	_, gone := v.defs["Custom.Kubesoc.Gone"]
	assert.False(t, gone, "an artifact the content no longer ships is deleted")
}
