// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package root

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/backup"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/config"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/devenv"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/version"
	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

var RootCmd = &cobra.Command{
	Use: "kubeaid-core",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// EnableTraverseRunHooks runs this for every command, including
		// cobra's hidden completion command — i.e. on every TAB press.
		if commandNeedsNoLogFile(cmd) {
			return
		}

		logsDirectory := resolveLogsDirectory()

		// 0700 on every directory this creates: the logs directory sits
		// inside a tree that also holds secrets.yaml, and MkdirAll applies
		// this mode to any parent it has to create — with an explicit
		// --configs-directory that includes the operator's directory itself.
		err := os.MkdirAll(logsDirectory, 0o700)
		assert.AssertErrNil(cmd.Context(), err, "Failed ensuring that logs directory exists")

		// Create logger.

		// PID in the name: the shared logs directory serves every run on
		// the machine, and same-second runs would otherwise O_TRUNC each
		// other's log.
		logFilePath := filepath.Join(
			logsDirectory,
			fmt.Sprintf("%s-%d.log", time.Now().UTC().Format(time.RFC3339), os.Getpid()),
		)
		logFile, err := os.OpenFile(logFilePath, //nolint:gosec // G302
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0o644,
		)
		if err != nil {
			log.Fatalf("Failed opening log file : %v", err)
		}

		globals.LogFile = logFile
		globals.LogFilePath = logFilePath

		logger.CreateLogger(globals.IsDebugModeEnabled, []io.Writer{logFile, os.Stdout})
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// commandNeedsNoLogFile reports whether cmd does no cluster work and must
// not leave a log file behind: shell completion fires on every TAB press,
// and help/version/the bare root only print text. Each would otherwise drop
// a file into the durable per-user logs directory — one distinct file per
// invocation (the PID is in the name), which nothing ever cleans up.
func commandNeedsNoLogFile(cmd *cobra.Command) bool {
	// The bare root command only prints help.
	if !cmd.HasParent() {
		return true
	}
	// Parents included: `completion bash` is named "bash".
	for c := cmd; c.HasParent(); c = c.Parent() {
		switch c.Name() {
		case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd,
			"help", "completion", "version":
			return true
		}
	}
	return false
}

// resolveLogsDirectory picks where this run's log file starts. The log
// opens before any config is parsed, so this is a best pre-parse guess at
// the run's outputs home, corrected by setup.Prepare once general.yaml has
// been read.
func resolveLogsDirectory() string {
	// The operator chose the config location themselves; everything this
	// run produces, the log included, goes there. A path that is a
	// cluster's own configs directory means the cluster home; anywhere
	// else means the directory itself.
	if dir := globals.ConfigsDirectory; dir != "" {
		return clusterdir.LogsDirForConfigs(dir)
	}

	// The operator named the cluster, so its directory is already decided —
	// the log starts there and never needs to move.
	if globals.ClusterName != "" {
		if dir, err := clusterdir.LogsDir(globals.ClusterName); err == nil {
			return dir
		}
	}

	// No flags at all: the run is refused once config resolution runs, but
	// the refusal itself deserves a log — it starts (and stays) in the
	// shared per-user logs directory.
	shared, err := clusterdir.SharedLogsDir()
	if err == nil {
		return shared
	}

	// No user config dir (HOME unset). Log under the working directory
	// rather than refusing to run; setup.Prepare fails with the real
	// cause before anything touches infrastructure. Printed before
	// CreateLogger runs, so slog's default stderr handler makes this
	// visible on the terminal.
	slog.Warn("No user config directory — logging under "+
		constants.OutputLogsDirectory+" instead",
		slog.String("error", err.Error()))
	return constants.OutputLogsDirectory
}

func init() {
	// Expose the binary's own version so pkg/ code can read it without
	// importing cmd/ (which would create a circular dependency).
	globals.KubeaidCLIVersion = version.Version

	// Subcommands.
	RootCmd.AddCommand(config.ConfigCmd)
	RootCmd.AddCommand(devenv.DevenvCmd)
	RootCmd.AddCommand(backup.BackupCmd)
	RootCmd.AddCommand(cluster.ClusterCmd)
	RootCmd.AddCommand(version.VersionCommand)

	// Flags.

	RootCmd.PersistentFlags().
		BoolVar(&globals.IsDebugModeEnabled, constants.FlagNameDebug, false, "Generate debug logs")

	RootCmd.PersistentFlags().
		StringVar(&globals.ConfigsDirectory,
			constants.FlagNameConfigsDirectory,
			constants.FlagNameConfigsDirectoryDefaultValue,
			"Directory holding the general and secrets config files; every output of the run"+
				" (kubeconfigs, logs, k3d config) lands in it too."+
				" Default: the cluster's own directory under ~/.config/kubeaid-cli, picked via --"+
				constants.FlagNameClusterName,
		)
	RootCmd.MarkPersistentFlagDirname(constants.FlagNameConfigsDirectory)

	RootCmd.PersistentFlags().
		StringVar(&globals.ClusterName,
			constants.FlagNameClusterName,
			"",
			"Name of the cluster whose config to use, from ~/.config/kubeaid-cli/<name>/configs."+
				" Needed only when several clusters have a saved config — with exactly one, it is"+
				" used automatically (ignored when --"+constants.FlagNameConfigsDirectory+" is given)",
		)
}
