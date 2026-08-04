// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteConfigFilesMatchesRender pins writeConfigFiles as a thin caller
// of Render: what it puts on disk must be byte-identical to what Render
// returns.
//
// Split out of pkg/render's golden test when Render moved there — the
// golden fixtures pin Render's output, this pins that the CLI's own
// disk-writing path does not diverge from it. Without this, writeConfigFiles
// could grow a transformation and only the CLI would be affected, silently,
// while the Obmondo API kept getting the untransformed bytes.
func TestWriteConfigFilesMatchesRender(t *testing.T) {
	cfg := &PromptedConfig{
		ClusterName:          "demo",
		KubeaidForkURL:       "https://github.com/Obmondo/KubeAid",
		KubeaidConfigForkURL: "git@github.com:acme/kubeaid-config.git",
		K8sVersion:           "v1.31.0",
	}

	wantGeneral, wantSecrets, err := Render(cfg)
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, writeConfigFiles(dir, cfg))

	gotGeneral, err := os.ReadFile(filepath.Join(dir, "general.yaml"))
	require.NoError(t, err)
	gotSecrets, err := os.ReadFile(filepath.Join(dir, "secrets.yaml"))
	require.NoError(t, err)

	assert.Equal(t, string(wantGeneral), string(gotGeneral),
		"writeConfigFiles general.yaml must be byte-identical to Render's output")
	assert.Equal(t, string(wantSecrets), string(gotSecrets),
		"writeConfigFiles secrets.yaml must be byte-identical to Render's output")
}
