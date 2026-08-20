// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster/delete"
	clusterSync "github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster/sync"
	"github.com/Obmondo/kubeaid-cli/cmd/kubeaid-core/root/cluster/upgrade"
	configSetup "github.com/Obmondo/kubeaid-cli/pkg/config/setup"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

var ClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage the lifecycle of a KubeAid managed K8s cluster",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if preparedByCommand(cmd) {
			return
		}
		prepareClusterCommand(cmd.Context())
	},
}

// preparedByCommand reports whether cmd, or an ancestor of it, prepares itself
// and so must not be prepared here first.
//
// Both mains set cobra.EnableTraverseRunHooks, so this hook and the
// subcommand's both run, parent first. BootstrapCmd resolves the Obmondo
// config onto disk inside its own hook and prepares afterwards; preparing here
// would run before that fetch and exit on the empty configs directory every
// --token run starts from. The chain is walked rather than compared so a
// subcommand added under bootstrap inherits the same treatment instead of
// silently getting the broken ordering back.
func preparedByCommand(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if current == BootstrapCmd {
			return true
		}
	}

	return false
}

// prepareClusterCommand parses and validates the cluster config, then sets up
// the temp directory. BootstrapCmd calls it from its own PersistentPreRun,
// once the Obmondo config is on disk.
func prepareClusterCommand(ctx context.Context) {
	cleanup, err := configSetup.Prepare(ctx)
	if err != nil {
		slog.ErrorContext(
			ctx, "Failed preparing config files",
			slog.String("error", err.Error()),
		)
		cleanup()
		os.Exit(1)
	}
	cobra.OnFinalize(cleanup)

	// Initialize temp directory.
	if err := utils.InitTempDir(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed initializing temp dir", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

var managementClusterName string

func init() {
	// Subcommands.
	ClusterCmd.AddCommand(BootstrapCmd)
	ClusterCmd.AddCommand(TestCmd)
	ClusterCmd.AddCommand(upgrade.UpgradeCmd)
	ClusterCmd.AddCommand(clusterSync.SyncCmd)
	ClusterCmd.AddCommand(delete.DeleteCmd)
	ClusterCmd.AddCommand(RecoverCmd)

	// Flags.

	ClusterCmd.PersistentFlags().
		BoolVar(&skipPRWorkflow, constants.FlagNameSkipPRWorkflow, false,
			"Skip the PR workflow and let KubeAid Bootstrap Script push changes directly to the default branch")

	ClusterCmd.PersistentFlags().
		StringVar(
			&managementClusterName,
			constants.FlagNameManagementClusterName,
			"",
			"Name of the local K3D management cluster. When omitted, defaults to "+
				constants.ManagementClusterNamePrefix+"<cluster-name> (from general.yaml)",
		)
}
