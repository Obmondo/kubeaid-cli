// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	kubeaidCoreRoot "github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	configSetup "github.com/Obmondo/kubeaid-bootstrap-script/pkg/config/setup"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/containerruntime"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/containerruntime/docker"
)

func main() {
	//nolint:reassign
	// By default, parent's PersistentPreRun gets overridden by a child's PersistentPreRun.
	// We want to disable this overriding behaviour and chain all the PersistentPreRuns.
	// REFERENCE : https://github.com/spf13/cobra/pull/2044.
	cobra.EnableTraverseRunHooks = true

	rootCmd := createRootCommand()

	err := rootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

// Constructs the root command.
func createRootCommand() *cobra.Command {
	// The interface should be same as that of KubeAid core.
	rootCmd := kubeaidCoreRoot.RootCmd

	rootCmd.Use = "kubeaid-cli"

	// Proxy cluster and devenv subcommands to containerized KubeAid core.
	for _, subCommand := range rootCmd.Commands() {
		subCommandName := subCommand.Name()

		if (subCommandName != "cluster") && (subCommandName != "devenv") {
			continue
		}

		// Unset PersistentPreRun and Run,
		// for this subcommand, and subcommands of this subcommand.
		unsetRunners(subCommand)

		// Proxy command execution to containerized KubeAid Core.
		subCommand.PersistentPreRun = proxyRun
	}

	return rootCmd
}

// Unsets PersistentPreRun and Run,
// for the given command, as well as, its subcommands.
func unsetRunners(command *cobra.Command) {
	command.PersistentPreRun = nil
	command.Run = func(cmd *cobra.Command, args []string) {}

	for _, subCommand := range command.Commands() {
		unsetRunners(subCommand)
	}
}

// proxyRun proxies the command execution to the KubeAid Core container.
func proxyRun(command *cobra.Command, _ []string) {
	ctx := command.Context()

	slog.InfoContext(ctx, "Proxying command execution to KubeAid Core container")

	if err := runProxy(ctx, command); err != nil {
		slog.ErrorContext(ctx, err.Error())
		os.Exit(1)
	}
}

func runProxy(ctx context.Context, command *cobra.Command) error {
	cleanup, err := configSetup.Prepare(ctx)
	if err != nil {
		cleanup()
		return fmt.Errorf("preparing config files: %w", err)
	}
	defer cleanup()

	managementClusterName, err := command.Flags().GetString(
		constants.FlagNameManagementClusterName,
	)
	if err != nil {
		return fmt.Errorf(
			"reading management cluster name from flag: %w", err,
		)
	}

	containerRuntime := docker.NewDocker(ctx)
	defer containerRuntime.CloseSocketConnection(ctx)

	kubeAidCoreContainer := &KubeAidCoreContainer{
		containerRuntime: containerRuntime,
		imagePullPolicy: containerruntime.ImagePullPolicy(
			config.ParsedGeneralConfig.ImagePullPolicy,
		),
		managementClusterName: managementClusterName,
		generalConfig:         config.ParsedGeneralConfig,
		commandArgs:           os.Args[1:],
	}
	kubeAidCoreContainer.Run(ctx)

	return nil
}
