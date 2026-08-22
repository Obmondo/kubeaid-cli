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
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

var RootCmd = &cobra.Command{
	Use: "kubeaid-core",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// The log file starts before any config is parsed, so which cluster
		// this run concerns is not known yet. With the default config
		// location the log goes to the shared per-user logs directory and
		// moves into the cluster's own directory once the cluster is known
		// (setup.RelocateLogFile); an explicit --configs-directory keeps
		// the historical working-directory-relative outputs/logs.
		logsDirectory := constants.OutputLogsDirectory
		if parser.UsingDefaultConfigsDirectory() {
			if shared, err := clusterdir.SharedLogsDir(); err == nil {
				logsDirectory = shared
			} else {
				// No user config dir (HOME unset). Fall back to the legacy
				// working-directory logs rather than refusing to run.
				slog.Warn("No user config directory — logging under "+
					constants.OutputLogsDirectory+" instead",
					slog.String("error", err.Error()))
			}
		}

		err := os.MkdirAll(logsDirectory, 0o750)
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
			"Path to the directory containing KubeAid Bootstrap Script general and secrets config files",
		)
	RootCmd.MarkPersistentFlagDirname(constants.FlagNameConfigsDirectory)

	RootCmd.PersistentFlags().
		StringVar(&globals.ClusterName,
			constants.FlagNameClusterName,
			"",
			"Name of the cluster whose config to use, from ~/.config/kubeaid-cli/<name>/configs"+
				" (ignored when --"+constants.FlagNameConfigsDirectory+" is given)",
		)
}
