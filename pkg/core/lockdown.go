// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"sort"
	gostrings "strings"

	"github.com/charmbracelet/huh"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"gopkg.in/yaml.v3"
	helmValues "helm.sh/helm/v3/pkg/cli/values"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	gitUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

const (
	// ciliumCCNPAPIVersion and ciliumCCNPKind are the GVK for the Cilium
	// CiliumClusterwideNetworkPolicy resource. Used when extracting the
	// unstructured object from the rendered Helm manifest without importing
	// the full cilium client.
	ciliumCCNPAPIVersion = "cilium.io/v2"
	ciliumCCNPKind       = "CiliumClusterwideNetworkPolicy"
)

// errLockdownDeclined is returned by promptLockdownConfirm when the operator
// explicitly declines. Callers distinguish this from a real error so they can
// info-log a graceful skip rather than warn-log a failure.
var errLockdownDeclined = errors.New("operator declined lockdown")

// nodeExternalIPs lists the cluster's Nodes and returns their ExternalIP
// addresses — the bare-metal public IPs, as Kubernetes itself knows them. The
// lockdown runs post-pivot with the cluster up, so the Node objects (populated
// by the cloud-controller-manager) are the live source of truth — no Robot API
// call or credentials needed. cpOnly restricts the result to control-plane nodes.
func nodeExternalIPs(ctx context.Context, c client.Client, cpOnly bool) ([]string, error) {
	var nodes coreV1.NodeList
	if err := c.List(ctx, &nodes); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var ips []string
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if cpOnly {
			if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; !isCP {
				continue
			}
		}
		for _, addr := range node.Status.Addresses {
			if addr.Type == coreV1.NodeExternalIP && addr.Address != "" {
				ips = append(ips, addr.Address)
			}
		}
	}
	return ips, nil
}

// lockdownInBootstrap runs the host-firewall lockdown step at the end of the
// bootstrap flow. It is gated: only runs on Hetzner bare-metal after clusterctl
// move. Operator declining is a graceful skip (info-logged); any actual failure
// is warn-logged and bootstrap continues — the cluster is already provisioned.
func lockdownInBootstrap(ctx context.Context, clusterClient client.Client, gitAuthMethod transport.AuthMethod) {
	if !config.UsingHetznerBareMetal() || !kubernetes.IsClusterctlMoveExecuted(ctx) {
		return
	}

	if err := runLockdownStep(ctx, clusterClient, gitAuthMethod); err != nil {
		if errors.Is(err, errLockdownDeclined) {
			slog.InfoContext(ctx, "Host firewall lockdown skipped by operator")
			return
		}
		slog.WarnContext(ctx, "Host firewall lockdown failed — cluster is provisioned; "+
			"re-run manually or apply the policy via ArgoCD",
			logger.Error(err))
	}
}

// runLockdownStep is the inner lockdown flow called by lockdownInBootstrap.
// It returns errLockdownDeclined when the operator declines the confirm prompt,
// or a wrapped error on any operational failure.
func runLockdownStep(ctx context.Context, clusterClient client.Client, gitAuthMethod transport.AuthMethod) error {
	if err := runLockdownPreFlights(ctx, clusterClient); err != nil {
		return fmt.Errorf("lockdown pre-flight: %w", err)
	}

	// Resolve the post-lockdown access hint while the cluster is still
	// reachable — applyCCNP below locks 6443 to the node IPs, after which this
	// client can no longer list nodes.
	accessLine := kubectlAccessLine(ctx, clusterClient)

	// Runs after bar.Finish() in the bootstrap flow, so the confirm prompt has
	// clean stdout — no progress-bar pause/resume needed.
	if err := promptLockdownConfirm(accessLine); err != nil {
		return err
	}

	if err := applyCCNP(ctx, clusterClient); err != nil {
		return fmt.Errorf("applying host-firewall CCNP: %w", err)
	}

	if err := raiseLockdownPR(ctx, gitAuthMethod, accessLine); err != nil {
		return fmt.Errorf("raising lockdown PR: %w", err)
	}
	return nil
}

// ---- pre-flights -------------------------------------------------------

// runLockdownPreFlights runs the ordered sequence of validation gates.
// Hard failures return an error; warnings are emitted via slog and do not
// prevent the lockdown from proceeding.
func runLockdownPreFlights(ctx context.Context, clusterClient client.Client) error {
	if err := checkNodeIPsPresent(ctx, clusterClient); err != nil {
		return err
	}
	warnIfSSHWorldOpen(ctx)
	return nil
}

// warnIfSSHWorldOpen emits a WARN when firewall.allowSshFrom is empty.
// SSH will remain world-open under the policy in that case (the world-entity
// rule adds port 22 when allowSshFrom is nil — same as bare-metal default),
// but making it explicit helps the operator understand what they're accepting.
func warnIfSSHWorldOpen(ctx context.Context) {
	h := config.ParsedGeneralConfig.Cloud.Hetzner
	if h == nil || h.BareMetal == nil {
		return
	}
	if len(h.BareMetal.Firewall.AllowSSHFrom) == 0 {
		slog.WarnContext(ctx,
			"firewall.allowSshFrom is empty — SSH (22/TCP) will remain "+
				"world-reachable after lockdown. "+
				"Set firewall.allowSshFrom in general.yaml to restrict SSH to known CIDRs.")
	}
}

// checkNodeIPsPresent errors when no Node has an ExternalIP. The rendered policy
// restricts 6443 to apiserverSourceCIDRs (the node public IPs); without at least
// one the rule is absent and kubelets can't reach the apiserver after lockdown.
func checkNodeIPsPresent(ctx context.Context, c client.Client) error {
	ips, err := nodeExternalIPs(ctx, c, false)
	if err != nil {
		return fmt.Errorf("reading node external IPs: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf(
			"no node ExternalIP found — at least one node public IP is needed so the " +
				"apiserverSourceCIDRs rule allows kubelet→apiserver traffic after lockdown")
	}
	slog.InfoContext(ctx, "Node public IPs present", slog.Int("count", len(ips)))
	return nil
}

// ---- confirm prompt ----------------------------------------------------

// ciliumValuesFile is a subset of the rendered values-cilium.yaml used only
// to extract the allowSshFrom field for the confirm summary.
type ciliumValuesFile struct {
	HostNetworkPolicy struct {
		AllowSSHFrom []string `yaml:"allowSshFrom"`
	} `yaml:"hostNetworkPolicy"`
}

// readAllowSshFrom reads allowSshFrom from the local values-cilium.yaml.
// On any error (file unreadable, field absent) it returns a descriptive
// fallback string so the confirm prompt is never blocked.
func readAllowSshFrom(valuesPath string) string {
	data, err := os.ReadFile(valuesPath) //nolint:gosec // operator-supplied repo layout path
	if err != nil {
		return "world-open (values-cilium.yaml unreadable)"
	}
	var v ciliumValuesFile
	if err := yaml.Unmarshal(data, &v); err != nil {
		return "world-open (values-cilium.yaml unreadable)"
	}
	if len(v.HostNetworkPolicy.AllowSSHFrom) == 0 {
		return "world-open (allowSshFrom not set)"
	}
	return fmt.Sprintf("restricted to: %v", v.HostNetworkPolicy.AllowSSHFrom)
}

// firstCPNodeIP returns a control-plane node's ExternalIP (sorted for
// determinism) for the SSH local-forward access hint, or the placeholder
// "<cp-node-ip>" when none can be determined. A CP node specifically, because
// the tunnel forwards 6443 — the apiserver runs on control-plane nodes only.
func firstCPNodeIP(ctx context.Context, c client.Client) string {
	ips, err := nodeExternalIPs(ctx, c, true)
	if err != nil || len(ips) == 0 {
		return "<cp-node-ip>"
	}
	sort.Strings(ips)
	return ips[0]
}

// kubectlAccessLine builds the post-lockdown cluster access instruction based
// on the configured VPN/SSH method. When NetBird is configured it returns the
// NetBird kubeconfig command; otherwise it constructs an SSH local-forward
// command using a control-plane node's public IP and the SSH key path.
func kubectlAccessLine(ctx context.Context, c client.Client) string {
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	if config.ParsedGeneralConfig.Cluster.NetBird != nil {
		return fmt.Sprintf("via NetBird — netbird kubernetes write-kubeconfig %s", clusterName)
	}

	// SSH local-forward path. SSHKeyPair is on HetznerConfig directly (not
	// under BareMetal), so only a nil HetznerConfig check is needed.
	h := config.ParsedGeneralConfig.Cloud.Hetzner
	sshKeyPath := "ssh-agent"
	if h != nil && !h.SSHKeyPair.UseSSHAgent && h.SSHKeyPair.PrivateKeyFilePath != "" {
		sshKeyPath = h.SSHKeyPair.PrivateKeyFilePath
	}

	cpIP := firstCPNodeIP(ctx, c)
	return fmt.Sprintf(
		"via SSH local-forward — ssh -L 6443:127.0.0.1:6443 -i %s root@%s  then  kubectl --server https://127.0.0.1:6443",
		sshKeyPath, cpIP,
	)
}

// promptLockdownConfirm presents the operator with a lockdown summary and
// requires explicit confirmation before any cluster or git change is made.
// Returns errLockdownDeclined when the operator explicitly declines, or a
// wrapped error when the prompt itself fails (e.g. no TTY).
func promptLockdownConfirm(accessLine string) error {
	clusterName := config.ParsedGeneralConfig.Cluster.Name
	ciliumValuesPath := path.Join(
		utils.GetClusterDir(),
		"argocd-apps", "values-cilium.yaml",
	)
	sshSource := readAllowSshFrom(ciliumValuesPath)

	description := fmt.Sprintf(
		"Cluster:   %s\n"+
			"SSH (22):  %s\n\n"+
			"Post-lockdown kubectl access:\n"+
			"  %s\n\n"+
			"Apiserver: 6443 → node public IPs only\n"+
			"Ingress:   80/443 world-reachable\n"+
			"Default:   all other ingress denied\n\n"+
			"Effect: CCNP applied now; PR persists it. Merge the PR before ArgoCD sync, or it reverts.",
		clusterName, sshSource, accessLine,
	)

	proceed := false
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Host Firewall Lockdown Summary").
				Description(description),
			huh.NewConfirm().
				Title("Proceed with host firewall lockdown?").
				Affirmative("Yes, apply CCNP and raise the PR").
				Negative("No, skip").
				Value(&proceed),
		),
	).Run(); err != nil {
		return fmt.Errorf("confirmation prompt: %w", err)
	}
	if !proceed {
		return errLockdownDeclined
	}
	return nil
}

// ---- apply CCNP --------------------------------------------------------

// applyCCNP renders the cilium chart with hostNetworkPolicy.enabled=true,
// extracts the CiliumClusterwideNetworkPolicy document, and applies it to
// the cluster using server-side apply (idempotent; handles both create and
// update without a get-then-patch race).
func applyCCNP(ctx context.Context, c client.Client) error {
	chartPath := path.Join(utils.GetKubeAidDir(), "argocd-helm-charts/cilium")
	ciliumValuesPath := path.Join(
		utils.GetClusterDir(),
		"argocd-apps", "values-cilium.yaml",
	)

	// Flip hostNetworkPolicy.enabled to true, then render that exact file — no
	// override, no value-merge (Helm coalesces the file over chart defaults
	// itself). raiseLockdownPR re-applies the same flip on the PR branch, so
	// what's applied here == what's committed there.
	if err := flipHostNetworkPolicyEnabled(ciliumValuesPath); err != nil {
		return fmt.Errorf("enabling hostNetworkPolicy in %q: %w", ciliumValuesPath, err)
	}

	slog.InfoContext(ctx, "Rendering cilium chart for host-firewall CCNP",
		slog.String("chart", chartPath),
		slog.String("values", ciliumValuesPath),
	)

	rendered, err := kubernetes.HelmRenderManifest(ctx, &kubernetes.HelmRenderArgs{
		ChartPath:   chartPath,
		ReleaseName: "cilium",
		Namespace:   "kube-system",
		Values: &helmValues.Options{
			ValueFiles: []string{ciliumValuesPath},
		},
	})
	if err != nil {
		return fmt.Errorf("rendering cilium chart: %w", err)
	}

	ccnp, err := extractCCNPFromManifest(rendered)
	if err != nil {
		return fmt.Errorf("extracting CCNP from rendered chart %q: %w", chartPath, err)
	}

	// Server-side apply: same strategy as ApplyManifestFromReader in apply.go.
	// Handles both create and update in one call; idempotent.
	if err := c.Patch(ctx, ccnp, client.Apply, client.ForceOwnership, client.FieldOwner("kubeaid-cli")); err != nil {
		return fmt.Errorf("server-side applying CCNP %q: %w", ccnp.GetName(), err)
	}
	slog.InfoContext(ctx, "Applied host-firewall CCNP via server-side apply",
		slog.String("name", ccnp.GetName()))
	return nil
}

// extractCCNPFromManifest splits a multi-document YAML manifest and returns
// the first document whose kind is CiliumClusterwideNetworkPolicy. A decode
// error on any document is returned immediately (not silently skipped) so
// template regressions surface as actionable errors rather than a confusing
// "no CCNP found" message.
func extractCCNPFromManifest(manifest string) (*unstructured.Unstructured, error) {
	multidocReader := k8sYAML.NewYAMLReader(bufio.NewReader(bytes.NewReader([]byte(manifest))))

	for {
		docBytes, err := multidocReader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("reading YAML document: %w", err)
		}

		trimmed := gostrings.TrimSpace(string(docBytes))
		if trimmed == "" || trimmed == "---" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if decodeErr := k8sYAML.NewYAMLOrJSONDecoder(
			gostrings.NewReader(trimmed), len(docBytes),
		).Decode(obj); decodeErr != nil {
			return nil, fmt.Errorf("decoding YAML document: %w", decodeErr)
		}

		if obj.GetKind() == ciliumCCNPKind {
			return obj, nil
		}
	}

	return nil, fmt.Errorf(
		"no %s document found in rendered manifest — "+
			"check that the cilium chart at the given path renders the host-firewall policy "+
			"when hostNetworkPolicy.enabled=true",
		ciliumCCNPKind,
	)
}

// ---- PR raise ----------------------------------------------------------

// raiseLockdownPR flips hostNetworkPolicy.enabled to true in the cluster's
// cilium values overlay, commits it on a new branch in kubeaid-config, and
// prints the PR URL with next-steps instructions.
func raiseLockdownPR(ctx context.Context, authMethod transport.AuthMethod, accessLine string) error {
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	// kubeaid-config is already cloned (bootstrap did it) — open it in place
	// rather than re-cloning. CreateAndCheckoutToBranch below resets to the
	// default branch, so the flip is re-applied on the PR branch just below.
	repo, err := goGit.PlainOpen(utils.GetKubeAidConfigDir())
	if err != nil {
		return fmt.Errorf("opening kubeaid-config repo: %w", err)
	}

	workTree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting kubeaid-config worktree: %w", err)
	}

	defaultBranch := gitUtils.GetDefaultBranchName(ctx, authMethod, repo)
	branchName := "enable-host-firewall-" + clusterName
	gitUtils.CreateAndCheckoutToBranch(ctx, repo, branchName, workTree, authMethod)

	ciliumValuesPath := path.Join(
		utils.GetClusterDir(),
		"argocd-apps", "values-cilium.yaml",
	)

	if err := flipHostNetworkPolicyEnabled(ciliumValuesPath); err != nil {
		// Already true or hand-edited: log and continue so the commit/push
		// still surfaces the PR branch even if the file was pre-flipped.
		slog.WarnContext(ctx,
			"hostNetworkPolicy.enabled already true or file hand-edited — skipping flip",
			logger.Error(err),
			slog.String("path", ciliumValuesPath),
		)
	}

	commitMsg := fmt.Sprintf(
		"feat(%s): enable Cilium host firewall\n\n"+
			"Flip hostNetworkPolicy.enabled from false to true.\n"+
			"ArgoCD applies the CiliumClusterwideNetworkPolicy once merged.\n"+
			"Host firewall CCNP applied live to the cluster before this commit.",
		clusterName)

	commitHash := gitUtils.AddCommitAndPushChanges(
		ctx, repo, workTree, branchName, authMethod,
		clusterName, commitMsg, defaultBranch,
	)

	if commitHash.IsZero() {
		slog.WarnContext(ctx,
			"No changes to commit — hostNetworkPolicy.enabled may already be true")
		return nil
	}

	prURL := gitUtils.BuildPRCompareURL(repo, defaultBranch, branchName)
	slog.InfoContext(ctx, "Lockdown PR branch pushed",
		slog.String("branch", branchName),
		slog.String("pr_url", prURL))

	printLockdownNextSteps(prURL, clusterName, accessLine)
	return nil
}

// flipHostNetworkPolicyEnabled patches the cluster's values-cilium.yaml
// in-place, changing "  enabled: false" to "  enabled: true". The template
// always renders exactly this string (two-space indent, colon, space, value),
// so a literal replace is correct and avoids a full YAML parse/re-serialise
// round-trip that would strip comments.
func flipHostNetworkPolicyEnabled(filePath string) error {
	data, err := os.ReadFile(filePath) //nolint:gosec // operator-supplied path via repo layout
	if err != nil {
		return fmt.Errorf("reading %q: %w", filePath, err)
	}

	const (
		disabledLine = "  enabled: false"
		enabledLine  = "  enabled: true"
	)

	original := string(data)
	patched := gostrings.Replace(original, disabledLine, enabledLine, 1)
	if patched == original {
		// Idempotent: already enabled is success. Only error when the key is
		// missing entirely (malformed / hand-edited overlay).
		if gostrings.Contains(original, enabledLine) {
			return nil
		}
		return fmt.Errorf(
			"neither %q nor %q found in %q — is the hostNetworkPolicy block present?",
			disabledLine, enabledLine, filePath)
	}

	if err := os.WriteFile(filePath, []byte(patched), 0o644); err != nil { //nolint:gosec // G306: file already exists
		return fmt.Errorf("writing patched %q: %w", filePath, err)
	}
	return nil
}

// printLockdownNextSteps prints the operator-facing next-steps message
// after the PR branch is pushed.
func printLockdownNextSteps(prURL, clusterName, accessLine string) {
	fmt.Printf("\n"+ //nolint:forbidigo // intentional operator-facing terminal output
		"Host firewall lockdown PR is ready.\n\n"+
		"  Branch:  enable-host-firewall-%s\n"+
		"  PR URL:  %s\n\n"+
		"Post-lockdown kubectl access:\n"+
		"  %s\n\n"+
		"Next steps:\n"+
		"  1. Review the diff — confirm hostNetworkPolicy.enabled flipped to true\n"+
		"     and apiserverSourceCIDRs lists every node public IP.\n"+
		"  2. Merge the PR before the next ArgoCD sync, or the CCNP reverts (fail-open).\n"+
		"  3. ArgoCD syncs the cilium app and applies the CCNP fleet-wide.\n"+
		"     Cluster connectivity is preserved via the identity rule (rule 1).\n"+
		"  4. Verify: kubectl get ccnp kubeaid-host-firewall\n"+
		"     should show Enforced=true on all nodes.\n",
		clusterName, prURL, accessLine,
	)
}
