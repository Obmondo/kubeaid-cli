// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
)

func TestGetParentDirPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "nested file", in: "/home/user/file.txt", want: "/home/user"},
		{name: "deeply nested file", in: "/home/user/deep/file.txt", want: "/home/user/deep"},
		{name: "file in current dir", in: "file.txt", want: "."},
		{name: "single segment under root", in: "/foo", want: "/"},
		{name: "trailing slash treated as a segment", in: "/foo/bar/", want: "/foo/bar"},
		{name: "empty path returns dot", in: "", want: "."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, GetParentDirPath(tc.in))
		})
	}
}

func TestToAbsolutePath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "tilde-slash expands to home dir",
			in:   "~/documents/file.txt",
			want: home + "/documents/file.txt",
		},
		{
			name: "absolute path is returned unchanged",
			in:   "/usr/local/bin",
			want: "/usr/local/bin",
		},
		{
			name: "relative path is anchored at cwd",
			in:   "some/relative/path",
			want: filepath.Join(cwd, "some/relative/path"),
		},
		{
			name: "dot resolves to cwd",
			in:   ".",
			want: cwd,
		},
		{
			name: "bare tilde expands to home dir",
			in:   "~",
			want: home,
		},
		{
			name: "tilde-username is not expanded",
			in:   "~someone/file",
			want: filepath.Join(cwd, "~someone/file"),
		},
		{
			name: "double slashes are collapsed",
			in:   "/usr//local///bin",
			want: "/usr/local/bin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToAbsolutePath(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetDownloadedStorageBucketContentsDir(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
	}{
		{name: "simple bucket name", bucket: "kubeaid-backups"},
		{name: "bucket name with dots", bucket: "company.snapshots"},
		{name: "bucket name with dashes", bucket: "obmondo-prod-2026"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GetDownloadedStorageBucketContentsDir(tc.bucket)
			assert.Equal(t, path.Join(constants.TempDirectory, "buckets", tc.bucket), got)
			assert.True(
				t,
				strings.HasPrefix(got, constants.TempDirectory),
				"path must be rooted under TempDirectory",
			)
		})
	}
}

func TestCreateIntermediateDirsForFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath func(root string) string
		wantDir  func(root string) string
	}{
		{
			name:     "creates a single missing parent directory",
			filePath: func(root string) string { return filepath.Join(root, "a", "file.txt") },
			wantDir:  func(root string) string { return filepath.Join(root, "a") },
		},
		{
			name:     "creates a deeply nested directory chain",
			filePath: func(root string) string { return filepath.Join(root, "a", "b", "c", "d", "file.txt") },
			wantDir:  func(root string) string { return filepath.Join(root, "a", "b", "c", "d") },
		},
		{
			name:     "no-op when parent already exists",
			filePath: func(root string) string { return filepath.Join(root, "file.txt") },
			wantDir:  func(root string) string { return root },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, CreateIntermediateDirsForFile(tc.filePath(root)))

			info, err := os.Stat(tc.wantDir(root))
			require.NoError(t, err)
			assert.True(t, info.IsDir(), "expected a directory at %s", tc.wantDir(root))
		})
	}
}

func TestMoveFile(t *testing.T) {
	tests := []struct {
		name                  string
		srcContent            string
		preExistingDstContent string
		wantDstContent        string
		forceFallback         bool
	}{
		{
			name:           "moves file content and removes source (rename fast path)",
			srcContent:     "hello",
			wantDstContent: "hello",
		},
		{
			name:                  "overwrites pre-existing destination (rename fast path)",
			srcContent:            "new content",
			preExistingDstContent: "old content",
			wantDstContent:        "new content",
		},
		{
			name:           "falls back to copy + delete when rename fails",
			srcContent:     "fallback content",
			wantDstContent: "fallback content",
			forceFallback:  true,
		},
		{
			name:                  "fallback path overwrites pre-existing destination",
			srcContent:            "new",
			preExistingDstContent: "old",
			wantDstContent:        "new",
			forceFallback:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.forceFallback {
				orig := renameFn
				t.Cleanup(func() { renameFn = orig })
				renameFn = func(string, string) error {
					return errors.New("simulated EXDEV")
				}
			}

			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			dst := filepath.Join(dir, "dst")

			require.NoError(t, os.WriteFile(src, []byte(tc.srcContent), 0o600))
			if tc.preExistingDstContent != "" {
				require.NoError(t, os.WriteFile(dst, []byte(tc.preExistingDstContent), 0o600))
			}

			require.NoError(t, MoveFile(src, dst))

			_, err := os.Stat(src)
			assert.True(t, os.IsNotExist(err), "source file should be removed")

			got, err := os.ReadFile(dst)
			require.NoError(t, err)
			assert.Equal(t, tc.wantDstContent, string(got))
		})
	}
}

func TestGetKubeAidDir(t *testing.T) {
	tests := []struct {
		name      string
		forkURL   string
		wantParts []string
	}{
		{
			name:      "github HTTPS URL",
			forkURL:   "https://github.com/Obmondo/kubeaid.git",
			wantParts: []string{"github.com", "Obmondo", "kubeaid"},
		},
		{
			name:      "scp-style SSH URL",
			forkURL:   "git@github.com:Obmondo/kubeaid-config.git",
			wantParts: []string{"github.com", "Obmondo", "kubeaid-config"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := git.ParseURL(tc.forkURL)
			require.NoError(t, err)

			orig := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = orig })
			config.ParsedGeneralConfig = &config.GeneralConfig{
				Forks: config.ForksConfig{
					KubeaidFork: config.KubeAidForkConfig{ParsedURL: parsed},
				},
			}

			got := GetKubeAidDir()
			for _, part := range tc.wantParts {
				assert.Contains(t, got, part)
			}
			assert.True(t, strings.HasPrefix(got, constants.TempDirectory))
		})
	}
}

func TestGetKubeAidConfigDir(t *testing.T) {
	tests := []struct {
		name      string
		forkURL   string
		wantParts []string
	}{
		{
			name:      "github HTTPS URL",
			forkURL:   "https://github.com/Obmondo/kubeaid-config.git",
			wantParts: []string{"github.com", "Obmondo", "kubeaid-config"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := git.ParseURL(tc.forkURL)
			require.NoError(t, err)

			orig := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = orig })
			config.ParsedGeneralConfig = &config.GeneralConfig{
				Forks: config.ForksConfig{
					KubeaidConfigFork: config.KubeaidConfigForkConfig{ParsedURL: parsed},
				},
			}

			got := GetKubeAidConfigDir()
			for _, part := range tc.wantParts {
				assert.Contains(t, got, part)
			}
		})
	}
}

func TestGetClusterDir(t *testing.T) {
	tests := []struct {
		name       string
		forkURL    string
		clusterDir string
		wantSuffix string
	}{
		{
			name:       "joins k8s and the configured directory name",
			forkURL:    "https://github.com/Obmondo/kubeaid-config.git",
			clusterDir: "prod-eu-west",
			wantSuffix: "/k8s/prod-eu-west",
		},
		{
			name:       "empty directory still produces /k8s/ suffix",
			forkURL:    "https://github.com/Obmondo/kubeaid-config.git",
			clusterDir: "",
			wantSuffix: "/k8s",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := git.ParseURL(tc.forkURL)
			require.NoError(t, err)

			orig := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = orig })
			config.ParsedGeneralConfig = &config.GeneralConfig{
				Forks: config.ForksConfig{
					KubeaidConfigFork: config.KubeaidConfigForkConfig{
						ParsedURL: parsed,
						Directory: tc.clusterDir,
					},
				},
			}

			got := GetClusterDir()
			assert.True(t,
				strings.HasSuffix(got, tc.wantSuffix),
				"got %q, want suffix %q", got, tc.wantSuffix,
			)
		})
	}
}
