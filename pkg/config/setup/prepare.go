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

// Prepare resolves the config source and parses the config files.
//
// Does NOT run the interactive prompt — that's the dedicated job of
// `kubeaid-cli config generate`. The earlier silent-prompt fallback
// here meant `cluster bootstrap` could surprise an operator with a TUI
// when they expected a parse failure, and the split also makes the
// command boundaries cleaner: `config generate` writes, `cluster
// bootstrap` reads.
func Prepare(ctx context.Context) error {
	if err := parser.ResolveConfigsDirectory(ctx); err != nil {
		return fmt.Errorf("resolving config source: %w", err)
	}

	exists, err := parser.ConfigFilesExist(globals.ConfigsDirectory)
	if err != nil {
		return fmt.Errorf("checking config files: %w", err)
	}
	if !exists {
		return notFoundError(globals.ConfigsDirectory)
	}

	parser.ParseConfigFiles(ctx, globals.ConfigsDirectory)

	// The cluster is known now, and with it the one place every output of
	// this run lands.
	home, err := outputsHome(ctx)
	if err != nil {
		return err
	}
	constants.UseOutputsHome(home)

	// The post-parse decision is the last word on where outputs live, the
	// log included: wherever the pre-parse guess opened it, the log now
	// moves next to the rest of the outputs. Every aligned flow hits the
	// already-in-place no-op.
	RelocateLogFile(ctx, constants.OutputLogsDirectory)

	return nil
}

// outputsHome returns the directory every output of this run lands in.
//
// Two config sources, two answers: a per-cluster configs directory (reached
// via --cluster-name, the Obmondo --token flow, or the operator spelling
// the path out) means the cluster's home under the per-user root; any other
// --configs-directory is the operator's own choice of location, and that
// choice applies to the outputs too — the per-user root is not involved
// at all.
func outputsHome(ctx context.Context) (string, error) {
	configsDirectory := globals.ConfigsDirectory

	if owner := clusterdir.OwnerOfConfigs(configsDirectory); owner != "" {
		// Case-folded: on the case-insensitive filesystems macOS and
		// Windows ship with, differently-cased names are one directory.
		if parsed := config.ParsedGeneralConfig.Cluster.Name; !strings.EqualFold(parsed, owner) {
			slog.ErrorContext(ctx,
				"general.yaml names a different cluster than its directory — proceeding, with outputs under the directory's cluster",
				slog.String("directory-cluster", owner),
				slog.String("general-yaml-cluster", parsed))
		}
		return clusterdir.Home(owner)
	}

	return filepath.Clean(configsDirectory), nil
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
