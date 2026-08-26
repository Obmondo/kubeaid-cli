// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package clusterdir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// withConfigHome points os.UserConfigDir at a temp tree for the test.
func withConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	return home
}

// saveConfig creates the tree a real config would occupy.
func saveConfig(t *testing.T, home, cluster string) {
	t.Helper()
	dir := filepath.Join(home, dirName, cluster, configsSubdir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, generalFileName), []byte("cluster:\n  name: "+cluster+"\n"), 0o600))
}

// ValidateName is the gate that stops a hostile or hand-edited general.yaml
// cluster name from becoming an arbitrary path segment under the per-user
// root — every rejection case here is a directory-traversal or collision
// that must never build a path.
func TestValidateName(t *testing.T) {
	valid := []string{
		"demo-01",
		"a",
		"Legacy_Prod", // pre-RFC-1123-rule names stay operable
		"Prod-EU-1",
		"x_y-z9",
		"logs2", // only the exact reserved name is refused
		"a234567890a234567890a234567890a234567890a234567890a234567890a23", // 63 chars
	}
	for _, name := range valid {
		require.NoError(t, ValidateName(name), "name %q", name)
	}

	invalid := []string{
		"",
		ReservedLogsName,
		"Logs", // same directory as the reserved name on case-insensitive filesystems
		"LOGS",
		"a234567890a234567890a234567890a234567890a234567890a234567890a234", // 64 chars
		"has.dots",
		"../evil",
		"foo/bar",
		`foo\bar`,
		"-leading-hyphen",
		"trailing-hyphen-",
		"_leading_underscore",
		"has space",
		"emoji💥",
	}
	for _, name := range invalid {
		require.Error(t, ValidateName(name), "name %q", name)
	}
}

func TestForIsPerClusterAndAbsolute(t *testing.T) {
	withConfigHome(t)

	dir, err := For("demo-01")
	require.NoError(t, err)

	require.Contains(t, dir, filepath.Join(dirName, "demo-01"))

	// A working-directory-relative path is the thing this exists to avoid:
	// it would put secrets.yaml inside whatever checkout the operator is in.
	require.True(t, filepath.IsAbs(dir))
}

func TestLogsDirIsInsideTheClusterHome(t *testing.T) {
	withConfigHome(t)

	logs, err := LogsDir("demo-01")
	require.NoError(t, err)

	home, err := Home("demo-01")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "logs"), logs)
}

// OwnerOfConfigs answers from the path alone, before any config is parsed,
// so it must say "" for everything that is not exactly a cluster's own
// configs directory under the per-user root.
func TestOwnerOfConfigs(t *testing.T) {
	home := withConfigHome(t)
	root := filepath.Join(home, dirName)

	require.Equal(t, "prod", OwnerOfConfigs(filepath.Join(root, "prod", "configs")))
	// Uncleaned input still resolves.
	require.Equal(t, "prod",
		OwnerOfConfigs(filepath.Join(root, "prod", "configs")+string(filepath.Separator)))
	// Either-case legacy-style names own directories too.
	require.Equal(t, "Legacy_Prod", OwnerOfConfigs(filepath.Join(root, "Legacy_Prod", "configs")))

	for _, path := range []string{
		"",
		"-", // stdin marker
		"outputs/configs",
		filepath.Join(root, "prod"), // the home, not its configs
		filepath.Join(root, "prod", "configs", "nested"),    // too deep
		filepath.Join(home, "elsewhere", "prod", "configs"), // outside the root
		filepath.Join(root, ReservedLogsName, "configs"),    // reserved name
		filepath.Join(root, "in.valid", "configs"),          // name that can't own a dir
		filepath.Join(root, "prod", "kubeconfigs"),          // wrong subdirectory
	} {
		require.Empty(t, OwnerOfConfigs(path), "path %q", path)
	}
}

func TestListReturnsSavedClustersSorted(t *testing.T) {
	home := withConfigHome(t)
	saveConfig(t, home, "prod-01")
	saveConfig(t, home, "demo-01")

	require.Equal(t, []string{"demo-01", "prod-01"}, List())
}

// A saved directory whose name the path builders refuse (left by an older
// CLI with laxer validation) must not be advertised: the operator would be
// handed a --cluster-name the very next command rejects.
func TestListSkipsNamesThePathBuildersRefuse(t *testing.T) {
	home := withConfigHome(t)
	saveConfig(t, home, "good-01")
	saveConfig(t, home, "bad.name")
	saveConfig(t, home, "trailing_")

	require.Equal(t, []string{"good-01"}, List())
}

// An aborted run can leave the tree behind with no config in it. Offering
// that name back would send the operator at a directory that cannot be used.
func TestListSkipsDirectoriesWithNoConfig(t *testing.T) {
	home := withConfigHome(t)
	saveConfig(t, home, "real-01")
	require.NoError(t, os.MkdirAll(filepath.Join(home, dirName, "abandoned", configsSubdir), 0o700))

	require.Equal(t, []string{"real-01"}, List())
}

// List only ever decorates a message the operator is already reading, so a
// missing or unreadable root must not turn into an error of its own.
func TestListIsEmptyWhenNothingHasBeenSaved(t *testing.T) {
	withConfigHome(t)
	require.Empty(t, List())
}

// LogsDirForConfigs is the one definition of log routing shared by the
// pre-parse placement and config generate's post-write relocation — the
// renamed-cluster case is why generate judges from where the files actually
// went, so the routing must answer from the path alone.
func TestLogsDirForConfigs(t *testing.T) {
	home := withConfigHome(t)
	root := filepath.Join(home, dirName)

	tests := []struct {
		name             string
		configsDirectory string
		want             string
	}{
		{
			name:             "a cluster's configs directory routes to the cluster's logs",
			configsDirectory: filepath.Join(root, "prod", configsSubdir),
			want:             filepath.Join(root, "prod", "logs"),
		},
		{
			// The prompt renamed the cluster mid-flow: the files landed
			// under the new name, so the log follows the files, not the
			// originally picked name.
			name:             "a renamed cluster's configs directory routes to the new name's logs",
			configsDirectory: filepath.Join(root, "prod-eu", configsSubdir),
			want:             filepath.Join(root, "prod-eu", "logs"),
		},
		{
			name:             "an operator's own directory holds its logs itself",
			configsDirectory: filepath.Join(home, "customer-a"),
			want:             filepath.Join(home, "customer-a", "logs"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, LogsDirForConfigs(tc.configsDirectory))
		})
	}
}
