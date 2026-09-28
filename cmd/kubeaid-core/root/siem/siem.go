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
	flagNameRotate            = "rotate"
	flagNameRemoveLegacy      = "remove-legacy-sealed-secrets"

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
  ` + core.SecurityOperationsSealedSecretsDir + `/security-operations/wazuh-{indexer,dashboard}-cred.yaml
  ` + core.SecurityOperationsSealedSecretsDir + `/wazuh-<code>/wazuh-{indexer-cred,dashboard-cred,api-cred,authd-pass}.yaml

It reads only forkURLs and cluster.securityOperations from the general config
(default: ` + generalConfigFileName + ` in --` + flagNameClusterDir + `); no secrets.yaml, no cloud
credentials. The Wazuh passwords live only in the sealed Secrets: a release
rendered before keeps its sealed files and password hashes, a new tenant gets
fresh passwords.

No silent rotation: a release that exists (its Application, values or any
sealed file is there) but whose state is incomplete (a sealed file, a password
hash or the cluster key is missing) is an error listing what is missing.
Restore the files from git, or rotate on purpose with --` + flagNameRotate + `=<release>
(security-operations or wazuh-<code>, comma separated or repeated) or --` + flagNameRotate + `
for every release. Rotated passwords reach the running Wazuh only when its
pods restart after the new sealed Secrets are synced.

Secret ownership: each SOC Application syncs its own sealed Secrets from
` + core.SecurityOperationsSealedSecretsDir + `/<namespace> (an extra source), outside the
sealed-secrets/ directory of the shared secrets app, which prunes. Every
SealedSecret carries argocd.argoproj.io/sync-options: Prune=false. Migrating a
cluster rendered before (Secrets under sealed-secrets/<namespace>/):

  1. siem render: copies each legacy file unchanged (same ciphertext) to its
     owned path, annotates both copies with Prune=false, and adds the extra
     source. Commit, push. Sync secrets (the live SealedSecrets now carry
     Prune=false), then root, security-operations, then each wazuh-<code>:
     they adopt the existing objects, nothing is deleted or re-created.
  2. siem render --` + flagNameRemoveLegacy + `: deletes the legacy copies. Commit,
     push, sync secrets (it drops them without pruning, Prune=false), then
     sync the SOC Applications once more so they are the owners.

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

		written, err := core.RenderSecurityOperationsWithOptions(cmd.Context(), dir,
			core.SecurityOperationsRenderOptions{Rotate: rotate})
		if err == nil && removeLegacy {
			var removed []string
			removed, err = core.RemoveSecurityOperationsLegacySealedSecrets(dir)
			for _, p := range removed {
				written = append(written, p+" (removed)")
			}
		}
		printWritten(cmd.OutOrStdout(), dir, written, core.SecurityOperationsLegacySealedSecretFiles(dir))
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

func printWritten(out io.Writer, dir string, written, legacy []string) {
	if len(written) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nWritten under %s:\n", dir)
	for _, p := range written {
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteString("\nReview the diff, then commit and push it to the kubeaid-config repository.\n")
	fmt.Fprintf(&b, "Sync order: root, security-operations, then the wazuh-<code> Apps (%s).\n",
		strings.Join(core.SecurityOperationsApplicationsInSyncOrder(), ", "))
	if len(legacy) > 0 {
		b.WriteString("\nThese sealed Secrets are still also in the shared secrets app's directory:\n")
		for _, p := range legacy {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteString("Sync secrets first (so the live SealedSecrets carry Prune=false), then the SOC Apps;\n")
		fmt.Fprintf(&b, "once they own the objects, run again with --%s and commit that separately.\n",
			flagNameRemoveLegacy)
	}
	_, _ = io.WriteString(out, b.String())
}

var (
	clusterDir        string
	generalConfig     string
	sealedSecretsCert string
	rotate            []string
	removeLegacy      bool
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

	RenderCmd.Flags().StringSliceVar(&rotate, flagNameRotate, nil,
		"Generate new credentials for these existing releases (security-operations, wazuh-<code>);"+
			" without a value (--"+flagNameRotate+"): every release")
	RenderCmd.Flags().Lookup(flagNameRotate).NoOptDefVal = core.SecurityOperationsRotateAll

	RenderCmd.Flags().BoolVar(&removeLegacy, flagNameRemoveLegacy, false,
		"Delete the SOC's sealed Secrets from the shared sealed-secrets/ directory (migration step 2,"+
			" only once the SOC Applications synced their own copies)")
}
