// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cluster

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/obmondo"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

var BootstrapCmd = &cobra.Command{
	Use: "bootstrap",

	Short: "Bootstrap a KubeAid managed K8s cluster",

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		// --connect-obmondo stands in for `config generate`: the Obmondo
		// portal has already collected every answer, so rather than
		// prompting, the rendered general.yaml and secrets.yaml are
		// downloaded and written before the bootstrap below reads them.
		var obmondoPaths *obmondo.WrittenPaths
		if len(connectObmondoToken) > 0 {
			obmondoPaths = fetchObmondoConfig(ctx, cmd)
		}

		core.BootstrapCluster(ctx, core.BootstrapClusterArgs{
			CreateDevEnvArgs: &core.CreateDevEnvArgs{
				ManagementClusterName:    managementClusterName,
				SkipMonitoringSetup:      skipMonitoringSetup,
				SkipPRWorkflow:           skipPRWorkflow,
				IsPartOfDisasterRecovery: false,
			},
			SkipClusterctlMove: skipClusterctlMove,
		})

		// Last, not at fetch time: bootstrap runs for many minutes and
		// scrolls a lot of output past, so a reminder printed up front
		// would be long gone by the time the operator is done.
		obmondo.PrintSecretsNotice(ctx, obmondoPaths)
	},
}

var skipMonitoringSetup,
	skipClusterctlMove bool

var connectObmondoToken,
	obmondoAPIURL string

// fetchObmondoConfig downloads this cluster's configuration and writes it,
// pointing globals.ConfigsDirectory at the result so the bootstrap that
// follows reads exactly what was just written. Returns where the files
// landed, for the end-of-run backup notice.
func fetchObmondoConfig(ctx context.Context, cmd *cobra.Command) *obmondo.WrittenPaths {
	config, err := obmondo.Fetch(ctx, obmondoAPIURL, connectObmondoToken)
	assert.AssertErrNil(ctx, err, "Failed fetching cluster configuration from Obmondo")

	clusterName, err := obmondo.ClusterName(config.GeneralYAML)
	assert.AssertErrNil(ctx, err, "Failed reading the cluster name from the fetched configuration")

	// The token decides which cluster this is, so --cluster-name cannot
	// redirect it. Disagreement is refused rather than silently resolved:
	// an operator who names one cluster and redeems another's token would
	// otherwise bootstrap the wrong cluster with no indication.
	if globals.ClusterName != "" && globals.ClusterName != clusterName {
		assert.AssertErrNil(ctx,
			fmt.Errorf("this token is for cluster %q, not %q", clusterName, globals.ClusterName),
			"Refusing to bootstrap: --"+constants.FlagNameClusterName+" does not match the token",
		)
	}

	// An explicit --configs-directory always wins. Without one the files go
	// to the per-cluster path under the user's config directory rather than
	// this flag's working-directory-relative default: secrets.yaml carries
	// cloud credentials and an mTLS private key, and an operator running
	// this is likely sitting inside a git checkout.
	if !cmd.Flags().Changed(constants.FlagNameConfigsDirectory) {
		globals.ConfigsDirectory, err = clusterdir.For(clusterName)
		assert.AssertErrNil(ctx, err, "Failed resolving where to write the cluster configuration")
	}

	written, err := obmondo.Write(globals.ConfigsDirectory, config)
	assert.AssertErrNil(ctx, err, "Failed writing the cluster configuration")

	obmondo.LogPaths(ctx, written)

	return written
}

func init() {
	// Flags.

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false,
			"Skip executing the 'clusterctl move' command",
		)

	// Defaulted from the environment so the token can be supplied without
	// landing in argv, which is world-readable via ps on a shared machine.
	BootstrapCmd.PersistentFlags().
		StringVar(&connectObmondoToken, constants.FlagNameConnectObmondo,
			os.Getenv(constants.EnvNameObmondoToken),
			"Fetch this cluster's configuration from Obmondo using the token issued by the portal"+
				" (also read from "+constants.EnvNameObmondoToken+")",
		)

	BootstrapCmd.PersistentFlags().
		StringVar(&obmondoAPIURL, constants.FlagNameObmondoAPIURL, obmondo.DefaultAPIURL,
			"Obmondo API to fetch the cluster configuration from",
		)
}
