// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	configSetup "github.com/Obmondo/kubeaid-cli/pkg/config/setup"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
)

var FixScrapeNamespacesCmd = &cobra.Command{
	Use: "fix-scrape-namespaces",

	Short: "Add any namespace missing prometheus-k8s RBAC to prometheus_scrape_namespaces",

	Long: `Runs the same check as "check-scrape-namespaces", and for every namespace it
finds, appends it to prometheus_scrape_namespaces in this cluster's
*-vars.jsonnet and rebuilds the kube-prometheus manifests.

Unlike "check-scrape-namespaces", this needs the cluster's config and the
KubeAid / KubeAid Config repos on disk (same as "cluster bootstrap"/"cluster
upgrade"), since it edits and rebuilds from those repos rather than just
querying the live cluster - so, unlike every other "monitoring" subcommand,
it takes --cluster-name/--configs-directory.

Only touches the local working tree: review the diff, then commit and push
yourself so ArgoCD picks it up. This is the standalone equivalent of the fix
"cluster bootstrap"/"cluster upgrade" already run automatically at the end of
every bootstrap or upgrade - use this to fix an existing cluster without
having to re-run one of those.`,

	Args: cobra.NoArgs,

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		prepareMonitoringFixCommand(cmd.Context())
	},

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		missing, err := core.CheckPrometheusScrapeNamespaces(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "Failed checking Prometheus scrape-namespace RBAC",
				slog.Any("err", err))
			os.Exit(1)
		}

		if len(missing) == 0 {
			fmt.Println( //nolint:forbidigo // operator-facing terminal output
				"OK: every namespace with a PodMonitor/ServiceMonitor has prometheus-k8s RBAC.",
			)
			return
		}

		if err := core.AddMissingScrapeNamespaces(ctx, missing); err != nil {
			slog.ErrorContext(ctx, "Failed adding missing scrape namespaces",
				slog.Any("namespaces", missing), slog.Any("err", err))
			os.Exit(1)
		}

		fmt.Printf( //nolint:forbidigo // operator-facing terminal output
			"Added to prometheus_scrape_namespaces and regenerated kube-prometheus manifests: %v\n"+
				"Review the diff, then commit and push so ArgoCD picks it up.\n",
			missing,
		)
	},
}

// prepareMonitoringFixCommand mirrors cluster.prepareClusterCommand: parses the cluster config
// and sets up the temp dir. Needed here (and only here, among monitoring's subcommands) because
// AddMissingScrapeNamespaces edits files in the KubeAid Config repo clone and reruns the
// kube-prometheus builder from the KubeAid repo clone, both resolved via ParsedGeneralConfig -
// check-scrape-namespaces only ever talks to the live cluster, so it doesn't need any of this.
func prepareMonitoringFixCommand(ctx context.Context) {
	if err := configSetup.Prepare(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed preparing config files", slog.Any("err", err))
		os.Exit(1)
	}

	if err := utils.InitTempDir(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed initializing temp dir", slog.Any("err", err))
		os.Exit(1)
	}
}
