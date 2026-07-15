// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"os"

	"github.com/spf13/cobra"

	backuppkg "github.com/Obmondo/kubeaid-cli/pkg/backup"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

const (
	flagNamespace  = "namespace"
	flagService    = "service"
	flagPort       = "port"
	flagKubeconfig = "kubeconfig"
	flagContext    = "context"
	flagOutput     = "output"
)

var (
	statusNamespace  string
	statusService    string
	statusPort       string
	statusKubeconfig string
	statusContext    string
	statusOutput     string
)

var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show PostgreSQL and Velero backup health from the backup-exporter",
	Long: `Reach the in-cluster backup-exporter Service through the Kubernetes
apiserver Service proxy, GET its /backups JSON and render a backup-health
report.

This is a normal kubectl-style client: it uses your current kube context
(standard kubeconfig resolution, honoring $KUBECONFIG, --kubeconfig and
--context). Your user needs 'get' on 'services/proxy' in the exporter's
namespace.

PostgreSQL rows are grouped per (namespace, cluster) with logical and WAL
columns; Velero rows are grouped per (namespace, resource, type, method). Use
-o wide to expand every age column, or -o json for the raw payload.`,
	Args: cobra.NoArgs,

	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		err := backuppkg.Status(ctx, backuppkg.Options{
			Kubeconfig: statusKubeconfig,
			Context:    statusContext,
			Namespace:  statusNamespace,
			Service:    statusService,
			Port:       statusPort,
			Output:     backuppkg.OutputFormat(statusOutput),
			Out:        os.Stdout,
		})
		assert.AssertErrNil(ctx, err, "Failed getting backup status")

		return nil
	},
}

func init() {
	// Flags.

	StatusCmd.Flags().StringVarP(&statusNamespace, flagNamespace, "n", backuppkg.DefaultNamespace,
		"Namespace the backup-exporter Service runs in")

	StatusCmd.Flags().StringVar(&statusService, flagService, backuppkg.DefaultService,
		"Name of the backup-exporter Service")

	StatusCmd.Flags().StringVar(&statusPort, flagPort, backuppkg.DefaultPort,
		"HTTP port the backup-exporter Service serves /backups on")

	StatusCmd.Flags().StringVar(&statusKubeconfig, flagKubeconfig, "",
		"Path to the kubeconfig file (default: standard resolution / $KUBECONFIG)")

	StatusCmd.Flags().StringVar(&statusContext, flagContext, "",
		"Kube context to use (default: the kubeconfig's current-context)")

	StatusCmd.Flags().StringVarP(&statusOutput, flagOutput, "o", string(backuppkg.OutputTable),
		"Output format: table, wide or json")
}
