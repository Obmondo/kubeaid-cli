// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package siem holds the `siem` commands: the security operations stack
// (cluster.securityOperations in general.yaml) of a KubeAid managed cluster.
package siem

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	configSetup "github.com/Obmondo/kubeaid-cli/pkg/config/setup"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

const (
	flagNameClusterDir        = "cluster-dir"
	flagNameSealedSecretsCert = "sealed-secrets-cert"
)

var SiemCmd = &cobra.Command{
	Use:   "siem",
	Short: "Manage the security operations (SIEM) stack of a KubeAid managed cluster",
}

var RenderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render only the security operations files into an existing cluster's config directory",
	Long: `Renders the files for cluster.securityOperations in general.yaml, and nothing else:

  argocd-apps/templates/security-operations.yaml   (central + one wazuh-<code> Application per tenant)
  argocd-apps/values-security-operations.yaml
  argocd-apps/values-wazuh-tenant.yaml
  sealed-secrets/security-operations/wazuh-{indexer,dashboard}-cred.yaml
  sealed-secrets/wazuh-<code>/wazuh-{indexer-cred,dashboard-cred,api-cred,authd-pass}.yaml

Missing Wazuh passwords and their hashes are generated into secrets.yaml first.
No git operation runs: review, commit and push the files yourself. Meant for
clusters whose other kubeaid-config files are maintained by hand.

Sealing needs the sealed-secrets controller's public certificate: pass it with
--` + flagNameSealedSecretsCert + ` (file or URL), or it is fetched from the controller
through the cluster in $KUBECONFIG (read-only).`,

	Args: cobra.NoArgs,

	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Parses general.yaml and secrets.yaml like every cluster command,
		// filling missing generated secrets into secrets.yaml.
		return configSetup.Prepare(cmd.Context())
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		dir := clusterDir
		if dir == "" {
			dir = utils.GetClusterDir()
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Errorf(
				"cluster directory %q does not exist - pass --%s with a local checkout of k8s/<cluster> in the kubeaid-config repository",
				dir, flagNameClusterDir,
			)
		}

		kubernetes.SealingCertSource = sealedSecretsCert

		written, err := core.RenderSecurityOperations(cmd.Context(), dir)
		printWritten(cmd.OutOrStdout(), dir, written)
		if err != nil {
			return err
		}
		if len(written) == 0 {
			return errors.New("nothing was rendered")
		}
		slog.InfoContext(cmd.Context(), "Rendered the security operations files",
			slog.String("cluster-dir", dir), slog.Int("files", len(written)))
		return nil
	},
}

func printWritten(out io.Writer, dir string, written []string) {
	if len(written) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nWritten under %s:\n", dir)
	for _, p := range written {
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteString("\nReview the diff, then commit and push it to the kubeaid-config repository.\n")
	b.WriteString("Sync order on an existing cluster: root, security-operations (creates the tenant namespaces), secrets, then the wazuh-<code> Apps.\n")
	_, _ = io.WriteString(out, b.String())
}

var (
	clusterDir        string
	sealedSecretsCert string
)

func init() {
	SiemCmd.AddCommand(RenderCmd)

	RenderCmd.Flags().StringVar(&clusterDir, flagNameClusterDir, "",
		"Local checkout of the cluster's directory (k8s/<cluster>) in the kubeaid-config repository."+
			" Default: the cluster directory in kubeaid-cli's working copy")
	RenderCmd.MarkFlagDirname(flagNameClusterDir)

	RenderCmd.Flags().StringVar(&sealedSecretsCert, flagNameSealedSecretsCert, "",
		"Sealed-secrets controller certificate (file path or URL) to seal with."+
			" Default: fetched from the controller in the cluster $KUBECONFIG points at")
	RenderCmd.MarkFlagFilename(flagNameSealedSecretsCert)
}
