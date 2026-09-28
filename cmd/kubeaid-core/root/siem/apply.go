// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// applyFlags drive runApply; apply and upgrade share them.
var applyFlags struct {
	branch        string
	currentBranch bool
	allowDirty    bool
	push          bool
	baseBranch    string
	sync          bool
	timeout       time.Duration
	skipPreflight bool
	strict        bool
	profile       string
	skipDNS       bool
	requireVelero bool
	message       string
}

// ApplyCmd is `siem apply`.
var ApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Render, commit, (push,) sync and verify the security operations stack",
	Long: `Runs the preflight checks, renders the security operations files into
--cluster-dir (the same render as 'siem render'), and commits exactly those
files on a branch of the kubeaid-config checkout (default kubesoc/apply-<time>;
--current-branch commits where you are). Nothing is ever force-pushed.

  --push   pushes the branch and prints the pull request link.
  --sync   syncs the Applications in order (root, security-operations,
           sealed-secrets, then each wazuh-<code>) through Argo CD, waits until
           each is Synced and Healthy, and prints 'siem status'. When the branch
           is not the revision Argo CD reads (securityOperations.configRevision,
           default HEAD of the default branch), it waits for you to merge first.

--dry-run renders into a scratch copy and prints the diff: no file, git or
cluster change.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := loadConfig(); err != nil {
			return err
		}
		return runApply(cmd, "apply", applyFlags.message)
	},
}

// runApply is apply's body after the config is loaded; upgrade and the
// tenant commands reuse it.
func runApply(cmd *cobra.Command, verb, message string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	cfg := config.ParsedGeneralConfig.Cluster.SecurityOperations

	if !applyFlags.skipPreflight {
		if err := preflight(ctx, out, cfg); err != nil {
			if !flagDryRun {
				return err
			}
			_, _ = fmt.Fprintf(out, "warning: %v (continuing: --dry-run)\n", err)
		}
	}
	if flagDryRun {
		return printRenderDiff(ctx, out)
	}

	repo, relCluster, err := configRepo(ctx)
	if err != nil {
		return err
	}
	branch, err := checkoutApplyBranch(ctx, repo, verb)
	if err != nil {
		return err
	}

	// The render is always called through core, so its behaviour (output
	// locations, rotation refusal) flows into apply, upgrade and tenant.
	// TODO(kubesoc-07): pass the rotation option through
	// core.RenderSecurityOperationsWithOptions and add a --rotate flag here.
	written, err := core.RenderSecurityOperations(ctx, clusterDir)
	printWritten(out, clusterDir, written)
	if err != nil {
		return err
	}

	if message == "" {
		message = "chore(security-operations): render " + config.ParsedGeneralConfig.Cluster.Name
	}
	commit, err := repo.Commit(ctx, message, relCluster)
	if err != nil {
		return err
	}
	if commit == "" {
		_, _ = fmt.Fprintln(out, "Nothing to commit: the rendered files were already committed.")
	} else {
		_, _ = fmt.Fprintf(out, "Committed %s on branch %s.\n", commit[:min(12, len(commit))], branch)
	}

	if applyFlags.push {
		if err := pushBranch(ctx, out, repo, branch); err != nil {
			return err
		}
	}
	if !applyFlags.sync {
		_, _ = fmt.Fprintln(out, "Not syncing (pass --sync): merge the change, then run 'kubeaid-cli siem apply --sync' or sync in Argo CD.")
		return nil
	}
	if !applyFlags.push && commit != "" {
		return errors.New("--sync needs the commit on the remote: pass --push as well")
	}
	if err := waitForMergeIfNeeded(cmd, cfg, branch, commit); err != nil {
		return err
	}

	apps, err := siemctl.ReadApplications(clusterDir, written)
	if err != nil {
		return err
	}
	order := siemctl.SyncOrder(apps)
	if err := confirm(cmd, fmt.Sprintf("Sync %s in the cluster of $KUBECONFIG?", strings.Join(order, ", "))); err != nil {
		return err
	}
	if err := syncInOrder(ctx, out, order); err != nil {
		return err
	}
	return printStatus(ctx, out, cfg, true)
}

// printRenderDiff renders into a scratch copy and prints the diff.
func printRenderDiff(ctx context.Context, out io.Writer) error {
	diff, _, err := siemctl.DryRunDiff(ctx, clusterDir, core.RenderSecurityOperations)
	if err != nil {
		return err
	}
	if diff == "" {
		_, _ = fmt.Fprintln(out, "No changes: the rendered files are up to date.")
		return nil
	}
	_, err = io.WriteString(out, diff)
	return err
}

// configRepo returns the kubeaid-config checkout --cluster-dir sits in and the
// cluster directory's path inside it, refusing uncommitted changes there
// unless --allow-dirty.
func configRepo(ctx context.Context) (siemctl.Git, string, error) {
	repo := siemctl.Git{Dir: clusterDir}
	top, err := repo.TopLevel(ctx)
	if err != nil {
		return repo, "", fmt.Errorf(
			"--%s must be inside a git checkout of the kubeaid-config repository: %w", flagNameClusterDir, err)
	}
	repo.Dir = top

	absCluster, err := filepath.Abs(clusterDir)
	if err != nil {
		return repo, "", err
	}
	relCluster, err := filepath.Rel(top, resolveSymlinks(absCluster))
	if err != nil {
		return repo, "", err
	}
	if applyFlags.allowDirty {
		return repo, relCluster, nil
	}

	dirty, err := repo.Dirty(ctx, relCluster)
	if err != nil {
		return repo, "", err
	}
	// The general config itself is expected to carry the change.
	if dirty = dropLine(dirty, generalConfigFileName); dirty != "" {
		return repo, "", fmt.Errorf(
			"uncommitted changes under %s would end up in the commit:\n%s\ncommit or stash them, or pass --allow-dirty",
			relCluster, dirty)
	}
	return repo, relCluster, nil
}

// checkoutApplyBranch checks out the branch the render is committed on.
func checkoutApplyBranch(ctx context.Context, repo siemctl.Git, verb string) (string, error) {
	if applyFlags.currentBranch {
		return repo.CurrentBranch(ctx)
	}
	branch := applyFlags.branch
	if branch == "" {
		branch = siemctl.DefaultBranchName(verb, time.Now())
	}
	return branch, repo.CheckoutBranch(ctx, branch)
}

// pushBranch pushes the branch and prints the pull request link.
func pushBranch(ctx context.Context, out io.Writer, repo siemctl.Git, branch string) error {
	if err := repo.Push(ctx, branch); err != nil {
		return err
	}
	origin, err := repo.OriginURL(ctx)
	if err != nil || branch == applyFlags.baseBranch {
		return nil //nolint:nilerr // without a usable origin there is just no link to print
	}
	if u := siemctl.CompareURL(origin, applyFlags.baseBranch, branch); u != "" {
		_, _ = fmt.Fprintf(out, "Open a pull request: %s\n", u)
	}
	return nil
}

// waitForMergeIfNeeded blocks until the operator merged the branch, when Argo
// CD reads another revision than the one just committed.
func waitForMergeIfNeeded(cmd *cobra.Command, cfg *config.SecurityOperationsConfig, branch, commit string) error {
	tracked := cfg.ConfigRevision
	if tracked == "" || tracked == "HEAD" {
		tracked = applyFlags.baseBranch
	}
	if branch == tracked || commit == "" {
		return nil
	}
	return waitForEnter(cmd, fmt.Sprintf(
		"Argo CD reads %q, the change is on %q: merge it first.", tracked, branch))
}

// syncInOrder syncs each Application through the existing Argo CD code and
// waits until it is Healthy.
func syncInOrder(ctx context.Context, out io.Writer, order []string) error {
	if err := kubernetes.RecreateArgoCDApplicationClient(ctx, nil); err != nil {
		return fmt.Errorf("connecting to Argo CD: %w", err)
	}
	for _, app := range order {
		_, _ = fmt.Fprintf(out, "Syncing %s ...\n", app)
		if err := kubernetes.SyncArgoCDApp(ctx, app, nil); err != nil {
			return fmt.Errorf("syncing %s: %w", app, err)
		}
		wctx, cancel := context.WithTimeout(ctx, applyFlags.timeout)
		err := kubernetes.WaitForArgoCDAppHealthy(wctx, app)
		cancel()
		if err != nil {
			return fmt.Errorf("waiting for %s to be Healthy: %w", app, err)
		}
	}
	return nil
}

func preflight(ctx context.Context, out io.Writer, cfg *config.SecurityOperationsConfig) error {
	c, err := newClients(ctx)
	if err != nil {
		return err
	}
	checks := siemctl.RunPreflight(ctx, siemctl.PreflightOptions{
		Kube:          c.kube,
		Dynamic:       c.dynamic,
		Config:        cfg,
		Profile:       applyFlags.profile,
		RequireVelero: applyFlags.requireVelero,
		SkipDNS:       applyFlags.skipDNS,
	})
	if err := siemctl.PrintChecks(out, checks); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out)
	if siemctl.PreflightFailed(checks, applyFlags.strict) {
		return errors.New("preflight failed")
	}
	return nil
}

func dropLine(porcelain, suffix string) string {
	var kept []string
	for _, l := range strings.Split(porcelain, "\n") {
		if l != "" && !strings.HasSuffix(l, suffix) {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

func resolveSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// addApplyFlags adds the flags runApply reads.
func addApplyFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&applyFlags.branch, "branch", "", "Branch to commit on (created from HEAD when missing). Default: kubesoc/<command>-<UTC time>")
	f.BoolVar(&applyFlags.currentBranch, "current-branch", false, "Commit on the checked out branch")
	f.BoolVar(&applyFlags.allowDirty, "allow-dirty", false, "Allow uncommitted changes in the cluster directory (they are committed too)")
	f.BoolVar(&applyFlags.push, "push", false, "Push the branch to origin and print the pull request link")
	f.StringVar(&applyFlags.baseBranch, "base", "main", "Default branch of the kubeaid-config repository (pull request target)")
	f.BoolVar(&applyFlags.sync, "sync", false, "Sync the Applications in order through Argo CD and wait until they are Healthy")
	f.DurationVar(&applyFlags.timeout, "timeout", 20*time.Minute, "How long to wait for each Application to become Healthy")
	f.StringVar(&applyFlags.message, "message", "", "Commit message")
	addPreflightFlags(cmd)
	addYesFlag(cmd, "syncing")
	addDryRunFlag(cmd, "Print the diff of the render and change nothing")
}

func addPreflightFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	if cmd != PreflightCmd {
		f.BoolVar(&applyFlags.skipPreflight, "skip-preflight", false, "Skip the preflight checks")
	}
	f.BoolVar(&applyFlags.strict, "strict", false, "Treat preflight warnings as failures")
	f.StringVar(&applyFlags.profile, "profile", siemctl.ProfileStandard, "Sizing profile for the capacity check: single or standard")
	f.BoolVar(&applyFlags.skipDNS, "skip-dns", false, "Skip the DNS resolution check")
	f.BoolVar(&applyFlags.requireVelero, "require-velero", false, "Fail when Velero (backups) is not installed")
}

// PreflightCmd is `siem preflight`.
var PreflightCmd = &cobra.Command{
	Use:   "preflight",
	Short: "Check the cluster is ready for the security operations stack (read-only)",
	Long: `Checks, reading only: the cluster is reachable; Argo CD, sealed-secrets,
cert-manager, CloudNativePG and mariadb-operator (and Velero, a warning unless
--require-velero) have their CRDs and a running controller; the ClusterIssuer
exists; a default StorageClass (and sharedStorageClass, when set) exists; every
configured host name resolves (a warning: external-dns may create them later);
and the schedulable nodes roughly fit the stack for --profile. 'siem apply'
runs the same checks first. Exits non-zero on a failure (or a warning with --strict).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return preflight(cmd.Context(), cmd.OutOrStdout(), cfg)
	},
}

func init() {
	SiemCmd.AddCommand(ApplyCmd, PreflightCmd)
	addClusterDirFlags(ApplyCmd)
	addSealingFlag(ApplyCmd)
	addApplyFlags(ApplyCmd)

	addClusterDirFlags(PreflightCmd)
	addPreflightFlags(PreflightCmd)
}
