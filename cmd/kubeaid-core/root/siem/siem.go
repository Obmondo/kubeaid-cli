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
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

const (
	flagNameClusterDir        = "cluster-dir"
	flagNameGeneralConfig     = "general-config"
	flagNameSealedSecretsCert = "sealed-secrets-cert"

	// generalConfigFileName is kubeaid-cli's copy of general.yaml in a
	// cluster's kubeaid-config directory.
	generalConfigFileName = "kubeaid-cli.general.yaml"
)

var SiemCmd = &cobra.Command{
	Use:   "siem",
	Short: "Manage the security operations (SIEM) stack of a KubeAid managed cluster",
}

var RenderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render only the security operations files into an existing cluster's config directory",
	Long: `Renders the files for cluster.securityOperations, and nothing else:

  argocd-apps/templates/security-operations.yaml   (central + one wazuh-<code> Application per tenant)
  argocd-apps/values-security-operations.yaml
  argocd-apps/values-wazuh-tenant.yaml
  sealed-secrets/security-operations/wazuh-{indexer,dashboard}-cred.yaml
  sealed-secrets/wazuh-<code>/wazuh-{indexer-cred,dashboard-cred,api-cred,authd-pass}.yaml

It reads only forkURLs and cluster.securityOperations from the general config
(default: ` + generalConfigFileName + ` in --` + flagNameClusterDir + `); no secrets.yaml, no cloud
credentials. The Wazuh passwords live only in the sealed Secrets: a release
rendered before keeps its sealed files and password hashes, a new tenant (or
one whose files are missing) gets fresh passwords.

No git operation runs: review, commit and push the files yourself. Meant for
clusters whose other kubeaid-config files are maintained by hand.

Sealing needs the sealed-secrets controller's public certificate: pass it with
--` + flagNameSealedSecretsCert + ` (file or URL), or it is fetched from the controller
through the cluster in $KUBECONFIG (read-only).`,

	Args: cobra.NoArgs,

	RunE: func(cmd *cobra.Command, args []string) error {
		dir := clusterDir
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Errorf(
				"cluster directory %q does not exist - pass --%s with a local checkout of k8s/<cluster> in the kubeaid-config repository",
				dir, flagNameClusterDir,
			)
		}

		generalConfigPath := generalConfig
		if generalConfigPath == "" {
			generalConfigPath = filepath.Join(dir, generalConfigFileName)
		}
		if err := parser.LoadSecurityOperationsConfig(generalConfigPath); err != nil {
			return err
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
	generalConfig     string
	sealedSecretsCert string
)

func init() {
	SiemCmd.AddCommand(RenderCmd)

	RenderCmd.Flags().StringVar(&clusterDir, flagNameClusterDir, "",
		"Local checkout of the cluster's directory (k8s/<cluster>) in the kubeaid-config repository")
	RenderCmd.MarkFlagDirname(flagNameClusterDir)
	_ = RenderCmd.MarkFlagRequired(flagNameClusterDir)

	RenderCmd.Flags().StringVar(&generalConfig, flagNameGeneralConfig, "",
		"General config with cluster.securityOperations and forkURLs. Default: "+
			generalConfigFileName+" in --"+flagNameClusterDir)
	RenderCmd.MarkFlagFilename(flagNameGeneralConfig)

	RenderCmd.Flags().StringVar(&sealedSecretsCert, flagNameSealedSecretsCert, "",
		"Sealed-secrets controller certificate (file path or URL) to seal with."+
			" Default: fetched from the controller in the cluster $KUBECONFIG points at")
	RenderCmd.MarkFlagFilename(flagNameSealedSecretsCert)
}
