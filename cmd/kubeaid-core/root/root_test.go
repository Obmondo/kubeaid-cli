// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package root

import (
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// Commands that do no cluster work must not leave a log file: shell
// completion runs the root hook on every TAB press, so getting this wrong
// grows the per-user logs directory by one file per keystroke.
func TestCommandNeedsNoLogFile(t *testing.T) {
	root := &cobra.Command{Use: "kubeaid-core"}
	clusterCmd := &cobra.Command{Use: "cluster"}
	bootstrap := &cobra.Command{Use: "bootstrap"}
	clusterCmd.AddCommand(bootstrap)
	versionCmd := &cobra.Command{Use: "version"}
	completion := &cobra.Command{Use: "completion"}
	completionBash := &cobra.Command{Use: "bash"}
	completion.AddCommand(completionBash)
	complete := &cobra.Command{Use: cobra.ShellCompRequestCmd}
	root.AddCommand(clusterCmd, versionCmd, completion, complete)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"bare root only prints help", root, true},
		{"version is informational", versionCmd, true},
		{"hidden completion command fires on every TAB press", complete, true},
		{"completion subcommands are covered via their parent", completionBash, true},
		{"cluster does real work", clusterCmd, false},
		{"nested cluster subcommands do real work", bootstrap, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandNeedsNoLogFile(tc.cmd); got != tc.want {
				t.Fatalf("commandNeedsNoLogFile(%s) = %v, want %v", tc.cmd.Name(), got, tc.want)
			}
		})
	}
}

// Mutates globals; not parallel. XDG_CONFIG_HOME is pointed at a temp dir
// so clusterdir resolves under the test's control.
func TestResolveLogsDirectory(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	origConfigsDirectory := globals.ConfigsDirectory
	origClusterName := globals.ClusterName
	t.Cleanup(func() {
		globals.ConfigsDirectory = origConfigsDirectory
		globals.ClusterName = origClusterName
	})

	root := filepath.Join(configHome, "kubeaid-cli")

	tests := []struct {
		name             string
		configsDirectory string
		clusterNameFlag  string
		want             string
	}{
		{
			name:             "no flags starts in the shared logs directory",
			configsDirectory: constants.FlagNameConfigsDirectoryDefaultValue,
			want:             filepath.Join(root, clusterdir.ReservedLogsName),
		},
		{
			name:             "cluster-name flag starts directly in the cluster's logs",
			configsDirectory: constants.FlagNameConfigsDirectoryDefaultValue,
			clusterNameFlag:  "prod",
			want:             filepath.Join(root, "prod", "logs"),
		},
		{
			name:             "explicit path at a cluster's configs starts in that cluster's logs",
			configsDirectory: filepath.Join(root, "prod", "configs"),
			want:             filepath.Join(root, "prod", "logs"),
		},
		{
			name: "operator's own directory holds its logs too",
			// The operator chose the location; everything the run
			// produces, the log included, goes there.
			configsDirectory: filepath.Join(configHome, "customer-a"),
			want:             filepath.Join(configHome, "customer-a", "logs"),
		},
		{
			name: "cluster-name flag is ignored next to an explicit path",
			// Matches the flag's documented contract and outputsHome: the
			// spelled-out directory is the home.
			configsDirectory: filepath.Join(configHome, "customer-a"),
			clusterNameFlag:  "prod",
			want:             filepath.Join(configHome, "customer-a", "logs"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			globals.ConfigsDirectory = tc.configsDirectory
			globals.ClusterName = tc.clusterNameFlag

			if got := resolveLogsDirectory(); got != tc.want {
				t.Fatalf("resolveLogsDirectory() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Mutates globals and unsets HOME/XDG_CONFIG_HOME; not parallel.
func TestResolveLogsDirectoryWithoutUserConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	origConfigsDirectory := globals.ConfigsDirectory
	origClusterName := globals.ClusterName
	t.Cleanup(func() {
		globals.ConfigsDirectory = origConfigsDirectory
		globals.ClusterName = origClusterName
	})
	globals.ConfigsDirectory = constants.FlagNameConfigsDirectoryDefaultValue
	globals.ClusterName = ""

	// No per-user config root exists, so the log falls back to the
	// working-directory default rather than refusing to run;
	// setup.Prepare later fails with the real cause.
	if got := resolveLogsDirectory(); got != constants.OutputLogsDirectory {
		t.Fatalf("resolveLogsDirectory() = %q, want %q", got, constants.OutputLogsDirectory)
	}
}
