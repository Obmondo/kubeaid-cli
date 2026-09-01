// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package monitoring

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/core"
)

var CheckScrapeNamespacesCmd = &cobra.Command{
	Use: "check-scrape-namespaces",

	Short: "Find namespaces with a PodMonitor/ServiceMonitor but missing prometheus-k8s RBAC",

	Long: `Lists every namespace that has a PodMonitor or ServiceMonitor but no
"prometheus-k8s" Role granting Prometheus list/watch access to it.

A namespace in this state was left out of prometheus_scrape_namespaces in the
cluster's kube-prometheus *-vars.jsonnet: Prometheus discovers the monitor
object fine, but is Forbidden from listing/watching pods or services in that
namespace, which fires PrometheusKubernetesListWatchFailures permanently
until the namespace is added there and kube-prometheus is rebuilt.

Runs against whatever cluster your current kubeconfig context points at,
exactly like kubectl.`,

	Args: cobra.NoArgs,

	Run: func(cmd *cobra.Command, args []string) {
		core.MonitoringScrapeNamespacesCheck(cmd.Context())
	},
}
