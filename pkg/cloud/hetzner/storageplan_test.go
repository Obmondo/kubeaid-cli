// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// TestPersistApprovedZFSSize covers the post-approval write-back into
// general.yaml. The yaml.Node tree manipulation is the load-bearing
// piece here — a bug in upsertYAMLIntField could corrupt the
// operator's config file, so each branch (insert / overwrite /
// preserve-comments / mirror-all-three-positions) gets its own subtest.
func TestPersistApprovedZFSSize(t *testing.T) {
	tests := []struct {
		name          string
		generalYAML   string
		size          int
		wantContains  []string
		wantUnchanged []string // substrings that must survive the rewrite
	}{
		{
			name: "inserts zfs.size when bareMetal exists but lacks zfs",
			generalYAML: `cloud:
  hetzner:
    bareMetal:
      wipeDisks: false
      installImage:
        imagePath: /foo
`,
			size:         220,
			wantContains: []string{"zfs:", "size: 220"},
			wantUnchanged: []string{
				"wipeDisks: false",
				"imagePath: /foo",
			},
		},
		{
			name: "overwrites existing zfs.size in place",
			generalYAML: `cloud:
  hetzner:
    bareMetal:
      wipeDisks: false
      zfs:
        size: 200
`,
			size:         440,
			wantContains: []string{"size: 440"},
			wantUnchanged: []string{
				"wipeDisks: false",
			},
		},
		{
			name: "preserves '# Robot main IP' comments and other host metadata",
			generalYAML: `cloud:
  hetzner:
    bareMetal:
      wipeDisks: false
    controlPlane:
      bareMetal:
        bareMetalHosts:
          # Robot main IP: 5.5.5.1
          - serverID: "1455726"
            privateIP: 10.0.1.1
`,
			size:         220,
			wantContains: []string{"size: 220"},
			wantUnchanged: []string{
				"# Robot main IP: 5.5.5.1",
				`serverID: "1455726"`,
				"privateIP: 10.0.1.1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "general.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.generalYAML), 0o600))

			// persistApprovedZFSSize keys off
			// config.GetGeneralConfigFilePath, which reads
			// globals.ConfigsDirectory. Point that at our temp dir
			// for the duration of the test.
			orig := globals.ConfigsDirectory
			t.Cleanup(func() { globals.ConfigsDirectory = orig })
			globals.ConfigsDirectory = dir

			require.NoError(t, persistApprovedZFSSize(t.Context(), tc.size))

			out, err := os.ReadFile(path)
			require.NoError(t, err)
			rewritten := string(out)

			for _, want := range tc.wantContains {
				assert.Contains(t, rewritten, want)
			}
			for _, want := range tc.wantUnchanged {
				assert.Contains(t, rewritten, want, "rewrite must preserve %q", want)
			}

			// Sanity: the rewrite must still be valid YAML and the
			// value must round-trip through yaml.Unmarshal at the
			// expected struct path.
			parsed := &config.GeneralConfig{}
			//nolint:musttag
			require.NoError(t, yaml.Unmarshal(out, parsed),
				"rewritten general.yaml must parse cleanly")
			require.NotNil(t, parsed.Cloud.Hetzner, "cloud.hetzner must survive the rewrite")
			require.NotNil(t, parsed.Cloud.Hetzner.BareMetal, "bareMetal must survive the rewrite")
			assert.Equal(t, tc.size, parsed.Cloud.Hetzner.BareMetal.ZFS.Size,
				"zfs.size must round-trip through unmarshal")
		})
	}
}

// TestPersistApprovedZFSSizeMirrorsAllPositions covers the contract
// that triggered the bug report: a single approval value must land in
// all three positions kubeaid-cli + the chart read — top-level
// bareMetal (storage-plan generation), controlPlane.bareMetal (chart's
// KubeadmControlPlane), and every nodeGroups.bareMetal[i] (chart's
// per-host KubeadmConfig). The previous shape only wrote the
// top-level field, leaving controlPlane.bareMetal without zfs and
// silently divergent from nodeGroups.bareMetal.
func TestPersistApprovedZFSSizeMirrorsAllPositions(t *testing.T) {
	generalYAML := `cloud:
  hetzner:
    bareMetal:
      wipeDisks: false
    controlPlane:
      bareMetal:
        bareMetalHosts:
          - serverID: "1455726"
            privateIP: 10.0.1.1
    nodeGroups:
      hcloud: []
      bareMetal:
        - name: workers-a
          bareMetalHosts:
            - serverID: "1414813"
              privateIP: 10.0.1.4
        - name: workers-b
          bareMetalHosts:
            - serverID: "1454837"
              privateIP: 10.0.1.5
`
	dir := t.TempDir()
	path := filepath.Join(dir, "general.yaml")
	require.NoError(t, os.WriteFile(path, []byte(generalYAML), 0o600))

	orig := globals.ConfigsDirectory
	t.Cleanup(func() { globals.ConfigsDirectory = orig })
	globals.ConfigsDirectory = dir

	require.NoError(t, persistApprovedZFSSize(t.Context(), 220))

	out, err := os.ReadFile(path)
	require.NoError(t, err)

	parsed := &config.GeneralConfig{}
	//nolint:musttag
	require.NoError(t, yaml.Unmarshal(out, parsed))
	require.NotNil(t, parsed.Cloud.Hetzner)

	// Top-level bareMetal.
	require.NotNil(t, parsed.Cloud.Hetzner.BareMetal)
	assert.Equal(t, 220, parsed.Cloud.Hetzner.BareMetal.ZFS.Size,
		"top-level cloud.hetzner.bareMetal.zfs.size must be set")

	// controlPlane.bareMetal.
	require.NotNil(t, parsed.Cloud.Hetzner.ControlPlane.BareMetal,
		"controlPlane.bareMetal must survive the rewrite")
	assert.Equal(t, 220, parsed.Cloud.Hetzner.ControlPlane.BareMetal.ZFS.Size,
		"cloud.hetzner.controlPlane.bareMetal.zfs.size must also be set — this is the bug we're fixing")

	// nodeGroups.bareMetal[*]: every entry gets the value, not just one.
	require.Len(t, parsed.Cloud.Hetzner.NodeGroups.BareMetal, 2,
		"both node groups must survive the rewrite")
	for i, ng := range parsed.Cloud.Hetzner.NodeGroups.BareMetal {
		assert.Equal(t, 220, ng.ZFS.Size,
			"nodeGroups.bareMetal[%d].zfs.size must be set", i)
	}
}

// TestPersistApprovedZFSSizeHCloudOnlyConfig pins the graceful-degrade
// path: in an hcloud-only config (no controlPlane.bareMetal, no
// nodeGroups.bareMetal sequence), the helper must still write the
// top-level field without erroring on the absent positions.
func TestPersistApprovedZFSSizeHCloudOnlyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "general.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`cloud:
  hetzner:
    bareMetal:
      wipeDisks: false
    controlPlane:
      hcloud:
        machineType: cax21
    nodeGroups:
      hcloud:
        - name: workers
`), 0o600))

	orig := globals.ConfigsDirectory
	t.Cleanup(func() { globals.ConfigsDirectory = orig })
	globals.ConfigsDirectory = dir

	require.NoError(t, persistApprovedZFSSize(t.Context(), 220))

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(out), "zfs:")
	assert.Contains(t, string(out), "size: 220")
	assert.Contains(t, string(out), "machineType: cax21", "hcloud CP fields must survive")
}

// TestPersistApprovedZFSSizeMissingBareMetal pins the error path: if
// general.yaml has no cloud.hetzner.bareMetal block at all (e.g. an
// HCloud-only config), persistApprovedZFSSize returns an error
// instead of silently injecting one. The caller wraps this into the
// outer GenerateStoragePlans error so the operator sees what's wrong.
func TestPersistApprovedZFSSizeMissingBareMetal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "general.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`cloud:
  hetzner:
    controlPlane:
      regions: [hel1]
`), 0o600))

	orig := globals.ConfigsDirectory
	t.Cleanup(func() { globals.ConfigsDirectory = orig })
	globals.ConfigsDirectory = dir

	err := persistApprovedZFSSize(t.Context(), 220)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "bareMetal not found"),
		"error should name the missing path; got %q", err.Error())
}

// TestFindYAMLChild covers the navigator helper directly. Used as the
// safety net for any future rewrite that needs to walk a yaml.Node
// tree by key path.
func TestFindYAMLChild(t *testing.T) {
	doc := `
a:
  b:
    c: 42
  empty:
`
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(doc), &root))
	docRoot := root.Content[0]

	t.Run("walks present path to scalar", func(t *testing.T) {
		got := findYAMLChild(docRoot, "a", "b", "c")
		require.NotNil(t, got)
		assert.Equal(t, "42", got.Value)
	})
	t.Run("returns nil when key is absent", func(t *testing.T) {
		assert.Nil(t, findYAMLChild(docRoot, "a", "missing"))
	})
	t.Run("returns nil when a hop is the wrong kind", func(t *testing.T) {
		// `a.b.c` is a scalar; walking past it must fail cleanly.
		assert.Nil(t, findYAMLChild(docRoot, "a", "b", "c", "deeper"))
	})
}
