// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func TestSplitYAMLDocuments(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantDocs     int
		wantContains [][]string
		wantErr      bool
	}{
		{
			name:     "empty input yields zero documents",
			input:    "",
			wantDocs: 0,
		},
		{
			name:         "single document is returned",
			input:        "name: foo\nvalue: 1\n",
			wantDocs:     1,
			wantContains: [][]string{{"name: foo", "value: 1"}},
		},
		{
			name:         "two documents separated by ---",
			input:        "name: foo\n---\nname: bar\n",
			wantDocs:     2,
			wantContains: [][]string{{"name: foo"}, {"name: bar"}},
		},
		{
			name:         "leading separator before single document",
			input:        "---\nname: foo\n",
			wantDocs:     1,
			wantContains: [][]string{{"name: foo"}},
		},
		{
			name:    "malformed YAML returns error",
			input:   "key: [unclosed",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			docs, err := splitYAMLDocuments([]byte(tc.input))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, docs, tc.wantDocs)
			for i, expected := range tc.wantContains {
				for _, fragment := range expected {
					assert.Contains(t, string(docs[i]), fragment)
				}
			}
		})
	}
}

func TestCleanupTempConfigsDirectory(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) string
		wantRemoved bool
	}{
		{
			name: "removes temp dir created by resolver",
			setup: func(t *testing.T) string {
				dir := filepath.Join(t.TempDir(), "kubeaid-configs-abc")
				require.NoError(t, os.MkdirAll(dir, 0o700))
				return dir
			},
			wantRemoved: true,
		},
		{
			name: "leaves untouched a path outside os.TempDir",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantRemoved: false,
		},
		{
			name: "leaves untouched a path missing the kubeaid-configs prefix",
			setup: func(t *testing.T) string {
				dir, err := os.MkdirTemp("", "unrelated-dir-")
				require.NoError(t, err)
				t.Cleanup(func() { _ = os.RemoveAll(dir) })
				return dir
			},
			wantRemoved: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := globals.ConfigsDirectory
			t.Cleanup(func() { globals.ConfigsDirectory = original })

			path := tc.setup(t)
			globals.ConfigsDirectory = path

			CleanupTempConfigsDirectory()

			_, err := os.Stat(path)
			if tc.wantRemoved {
				assert.True(t, os.IsNotExist(err), "directory should be removed")
				return
			}
			assert.NoError(t, err, "directory should still exist")
		})
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestResolveFromStdin(t *testing.T) {
	const (
		generalDoc = "cluster:\n  name: test\n"
		secretsDoc = "aws:\n  accessKey: hunter2\n"
	)

	tests := []struct {
		name              string
		stdin             io.Reader
		wantErr           bool
		wantErrContains   string
		wantGeneral       string
		wantSecrets       string
		wantSecretsExists bool
	}{
		{
			name:              "single doc writes general.yaml and empty secrets.yaml",
			stdin:             strings.NewReader(generalDoc),
			wantGeneral:       "name: test",
			wantSecretsExists: true,
			wantSecrets:       "{}",
		},
		{
			name:              "two docs write both general.yaml and secrets.yaml",
			stdin:             strings.NewReader(generalDoc + "---\n" + secretsDoc),
			wantGeneral:       "name: test",
			wantSecretsExists: true,
			wantSecrets:       "accessKey: hunter2",
		},
		{
			name:            "empty stdin errors",
			stdin:           strings.NewReader(""),
			wantErr:         true,
			wantErrContains: "no data received from stdin",
		},
		{
			name:            "malformed YAML errors",
			stdin:           strings.NewReader("key: [unclosed"),
			wantErr:         true,
			wantErrContains: "parsing stdin YAML",
		},
		{
			name:            "stdin read failure errors",
			stdin:           errReader{err: io.ErrUnexpectedEOF},
			wantErr:         true,
			wantErrContains: "reading from stdin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalReader := stdinReader
			originalDir := globals.ConfigsDirectory
			t.Cleanup(func() {
				stdinReader = originalReader
				if globals.ConfigsDirectory != originalDir {
					_ = os.RemoveAll(globals.ConfigsDirectory)
					globals.ConfigsDirectory = originalDir
				}
			})

			stdinReader = tc.stdin

			err := resolveFromStdin(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				return
			}
			require.NoError(t, err)

			generalBytes, readErr := os.ReadFile(filepath.Join(globals.ConfigsDirectory, "general.yaml"))
			require.NoError(t, readErr)
			assert.Contains(t, string(generalBytes), tc.wantGeneral)

			if tc.wantSecretsExists {
				secretsBytes, readErr := os.ReadFile(filepath.Join(globals.ConfigsDirectory, "secrets.yaml"))
				require.NoError(t, readErr)
				assert.Contains(t, string(secretsBytes), tc.wantSecrets)
			}
		})
	}
}

func TestResolveConfigsDirectory(t *testing.T) {
	tests := []struct {
		name          string
		configsDir    string
		stdin         io.Reader
		wantSameDir   bool
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:        "local path is left untouched",
			configsDir:  "/some/local/path",
			wantSameDir: true,
		},
		{
			name:       "stdin sentinel triggers stdin read",
			configsDir: "-",
			stdin:      strings.NewReader("cluster:\n  name: test\n"),
		},
		{
			name:          "stdin sentinel with empty stream errors",
			configsDir:    "-",
			stdin:         strings.NewReader(""),
			wantErr:       true,
			wantErrSubstr: "no data received from stdin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalDir := globals.ConfigsDirectory
			originalReader := stdinReader
			t.Cleanup(func() {
				if globals.ConfigsDirectory != originalDir &&
					strings.HasPrefix(globals.ConfigsDirectory, os.TempDir()) {
					_ = os.RemoveAll(globals.ConfigsDirectory)
				}
				globals.ConfigsDirectory = originalDir
				stdinReader = originalReader
			})

			globals.ConfigsDirectory = tc.configsDir
			if tc.stdin != nil {
				stdinReader = tc.stdin
			}

			err := ResolveConfigsDirectory(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			if tc.wantSameDir {
				assert.Equal(t, tc.configsDir, globals.ConfigsDirectory)
			} else {
				assert.NotEqual(t, tc.configsDir, globals.ConfigsDirectory)
			}
		})
	}
}
