// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

// Prepare resolves the config source and parses the config files. Returns
// a cleanup function the caller runs at process finalization to remove
// temp config dirs created for stdin-based configs.
//
// Does NOT run the interactive prompt — that's the dedicated job of
// `kubeaid-cli config generate`. The earlier silent-prompt fallback
// here meant `cluster bootstrap` could surprise an operator with a TUI
// when they expected a parse failure, and the split also makes the
// command boundaries cleaner: `config generate` writes, `cluster
// bootstrap` reads.
func Prepare(ctx context.Context) (func(), error) {
	cleanup := parser.CleanupTempConfigsDirectory

	if err := parser.ResolveConfigsDirectory(ctx); err != nil {
		return cleanup, fmt.Errorf("resolving config source: %w", err)
	}

	exists, err := parser.ConfigFilesExist(globals.ConfigsDirectory)
	if err != nil {
		return cleanup, fmt.Errorf("checking config files: %w", err)
	}
	if !exists {
		return cleanup, notFoundError(globals.ConfigsDirectory)
	}

	parser.ParseConfigFiles(ctx, globals.ConfigsDirectory)

	// The cluster is known now. When the configs live in the cluster's own
	// directory, outputs — kubeconfigs, logs, the generated k3d config —
	// move in next to them. Anything else (legacy ./outputs/configs, stdin,
	// an explicit --configs-directory) keeps the historical
	// working-directory outputs.
	home, err := perClusterHome(ctx, globals.ConfigsDirectory)
	if err != nil {
		return cleanup, err
	}
	if home != "" {
		constants.UsePerClusterOutputs(home)
		RelocateLogFile(ctx, constants.OutputLogsDirectory)
	}

	return cleanup, nil
}

// perClusterHome returns the parsed cluster's home directory when
// configsDirectory is that cluster's own configs directory, and "" for the
// legacy layouts. Judged from where the configs actually are, not from the
// --configs-directory flag: the Obmondo --token flow rewrites the directory
// before Prepare runs, and a legacy ./outputs/configs holds real files at
// the flag's default value. A cluster name the path builders reject fails
// the run.
func perClusterHome(ctx context.Context, configsDirectory string) (string, error) {
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	expected, err := clusterdir.For(clusterName)
	if err != nil {
		return "", fmt.Errorf("invalid cluster name in general.yaml: %w", err)
	}

	if filepath.Clean(configsDirectory) != expected {
		if globals.ClusterName != "" && globals.ClusterName != clusterName {
			slog.ErrorContext(ctx,
				"general.yaml names a different cluster than --cluster-name — outputs stay under ./outputs/",
				slog.String("cluster-name-flag", globals.ClusterName),
				slog.String("general-yaml-cluster", clusterName))
		}
		return "", nil
	}

	return filepath.Dir(expected), nil
}

// RelocateLogFile moves the current run's log file into directory, creating
// it first. The file was opened before the run knew which cluster it
// concerns; the open descriptor survives the rename, so logging simply
// continues into the moved file. Best-effort: a failure leaves the log
// where it is, logged at ERROR so it reaches the terminal — WARN goes only
// to the log file, the very artifact that failed to move.
func RelocateLogFile(ctx context.Context, directory string) {
	if globals.LogFilePath == "" {
		return
	}

	newPath := filepath.Join(directory, filepath.Base(globals.LogFilePath))
	if newPath == globals.LogFilePath {
		return
	}

	if err := os.MkdirAll(directory, 0o750); err != nil {
		slog.ErrorContext(ctx, "Couldn't create the cluster's logs directory — this run's log stays at its current path",
			slog.String("log", globals.LogFilePath), logger.Error(err))
		return
	}

	if err := os.Rename(globals.LogFilePath, newPath); err != nil {
		slog.ErrorContext(ctx, "Couldn't move the log file into the cluster's directory — this run's log stays at its current path",
			slog.String("log", globals.LogFilePath), slog.String("to", newPath),
			logger.Error(err))
		return
	}

	globals.LogFilePath = newPath
}

// notFoundError names the clusters that DO have a config on disk.
//
// Listed rather than offered as a picker: every caller of Prepare goes on to
// create, mutate or destroy real cloud infrastructure, and an arrow-key
// selection is an easy way to act on the wrong cluster. Naming it is cheap;
// the operator still has to type which one they meant.
func notFoundError(configsDirectory string) error {
	available := clusterdir.List()
	if len(available) == 0 {
		return fmt.Errorf(
			"config files not found under %q — run `kubeaid-cli config generate` first to create them",
			configsDirectory,
		)
	}

	return fmt.Errorf(
		"config files not found under %q\n\nclusters with a saved config:\n  %s\n\n"+
			"re-run with --%s <name>, or --%s <path>",
		configsDirectory,
		strings.Join(available, "\n  "),
		constants.FlagNameClusterName,
		constants.FlagNameConfigsDirectory,
	)
}
