// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"fmt"
	"log/slog"
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

	// Declared here rather than inherited from ClusterCmd because the Obmondo
	// config has to be on disk before the shared prepare step parses it, so
	// this calls the parent's logic itself once the fetch is done. ClusterCmd's
	// hook skips this command for that reason — see preparedByCommand.
	//
	// Ordering is the whole point: prepare exits with "config files not
	// found" when the directory is empty, which on a fresh machine is every
	// --token run, and the fetch used to live in Run, after it. Resolving
	// first also means the config that gets parsed and validated is the one
	// just written, not whatever happened to be on disk.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		// The Obmondo flags stand in for `config generate`: the portal has
		// already collected every answer, so the rendered general.yaml and
		// secrets.yaml are downloaded — or read back off disk — before the
		// bootstrap below reads them.
		switch {
		case bootstrapToken != "" && obmondoCertname != "":
			obmondoPaths = fetchObmondoConfig(ctx, cmd)

		// A token names no cluster on its own, and the API refuses a
		// redemption without one, so this is caught here rather than spent
		// on a request that cannot succeed.
		case bootstrapToken != "":
			assert.AssertErrNil(
				ctx,
				fmt.Errorf("--%s is required with --%s", constants.FlagNameCertname, constants.FlagNameToken),
				"Refusing to bootstrap: --"+constants.FlagNameToken+" does not say which cluster it is for",
			)

		case obmondoCertname != "":
			obmondoPaths = obmondoConfigFromDisk(ctx, cmd)
		}

		prepareClusterCommand(ctx)
	},

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

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

// obmondoPaths is set by PersistentPreRun and read by Run, which is why it
// is a package var rather than a local: cobra gives the two hooks no way to
// pass a value between them.
var obmondoPaths *obmondo.WrittenPaths

var skipMonitoringSetup,
	skipClusterctlMove bool

var bootstrapToken,
	obmondoCertname,
	obmondoAPIURL string

// obmondoConfigFromDisk continues from a configuration an earlier run already
// fetched. --certname without a token means "this cluster, whatever is on
// disk", so nothing is downloaded and no token has to still be valid.
//
// An explicit --configs-directory is checked in place; without one the
// per-cluster directories are searched for the certificate issued to this
// certname. Either way the certificate has to match, so a stale or unrelated
// config is refused here rather than surfacing as an authentication failure
// deep into bootstrap.
func obmondoConfigFromDisk(ctx context.Context, cmd *cobra.Command) *obmondo.WrittenPaths {
	if cmd.Flags().Changed(constants.FlagNameConfigsDirectory) {
		paths, err := obmondo.VerifyOnDisk(globals.ConfigsDirectory, obmondoCertname)
		assert.AssertErrNil(ctx, err, "Refusing to bootstrap: no usable configuration for --"+constants.FlagNameCertname)

		obmondo.LogReusedPaths(ctx, paths)
		return paths
	}

	directory, paths, err := obmondo.FindOnDisk(obmondoCertname)
	assert.AssertErrNil(ctx, err, "Refusing to bootstrap: no usable configuration for --"+constants.FlagNameCertname)

	globals.ConfigsDirectory = directory
	obmondo.LogReusedPaths(ctx, paths)
	return paths
}

// fetchObmondoConfig downloads this cluster's configuration and writes it,
// pointing globals.ConfigsDirectory at the result so the bootstrap that
// follows reads exactly what was just written. Returns where the files
// landed, for the end-of-run backup notice.
func fetchObmondoConfig(ctx context.Context, cmd *cobra.Command) *obmondo.WrittenPaths {
	// Looked up by certname before the request, because that is the only
	// handle available this early: the configs directory is otherwise
	// derived from the cluster name, which is inside the general.yaml that
	// has not been downloaded yet. Informational only — the fetch proceeds
	// either way and Write replaces what is there.
	existingDirectory, _, err := obmondo.FindOnDisk(obmondoCertname)
	if err == nil {
		slog.InfoContext(
			ctx, "Replacing the cluster configuration already on disk",
			slog.String("certname", obmondoCertname),
			slog.String("directory", existingDirectory),
		)
	}

	config, err := obmondo.Fetch(ctx, obmondoAPIURL, bootstrapToken, obmondoCertname)
	assert.AssertErrNil(ctx, err, "Failed fetching cluster configuration from Obmondo")

	clusterName, err := obmondo.ClusterName(config.GeneralYAML)
	assert.AssertErrNil(ctx, err, "Failed reading the cluster name from the fetched configuration")

	// The token decides which cluster this is, so --cluster-name cannot
	// redirect it. Disagreement is refused rather than silently resolved:
	// an operator who names one cluster and redeems another's token would
	// otherwise bootstrap the wrong cluster with no indication.
	if globals.ClusterName != "" && globals.ClusterName != clusterName {
		assert.AssertErrNil(
			ctx,
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
		BoolVar(
			&skipMonitoringSetup, constants.FlagNameSkipMonitoringSetup, false,
			"Skip KubePrometheus installation",
		)

	BootstrapCmd.PersistentFlags().
		BoolVar(
			&skipClusterctlMove, constants.FlagNameSkipClusterctlMove, false,
			"Skip executing the 'clusterctl move' command",
		)

	// Defaulted from the environment so the token can be supplied without
	// landing in argv, which is world-readable via ps on a shared machine.
	BootstrapCmd.PersistentFlags().
		StringVar(
			&bootstrapToken, constants.FlagNameToken,
			os.Getenv(constants.EnvNameToken),
			"Fetch this cluster's configuration from Obmondo using the bootstrap token issued by the portal"+
				" (also read from "+constants.EnvNameToken+")",
		)

	// Required with the token, not derivable from it: the API refuses a
	// redemption whose certname disagrees with the one the token was issued
	// for, and the portal hands out both together.
	BootstrapCmd.PersistentFlags().
		StringVar(
			&obmondoCertname, constants.FlagNameCertname,
			os.Getenv(constants.EnvNameCertname),
			"Certname the Obmondo token was issued for, as <cluster>.<customer-id>"+
				" (also read from "+constants.EnvNameCertname+")",
		)

	BootstrapCmd.PersistentFlags().
		StringVar(
			&obmondoAPIURL, constants.FlagNameObmondoAPIURL, obmondo.DefaultAPIURL,
			"Obmondo API to fetch the cluster configuration from",
		)
}
