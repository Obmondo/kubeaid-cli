// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type SyncCAPIClusterArgs struct {
	SkipPRWorkflow bool
}

// SyncCAPICluster reconciles a running ClusterAPI cluster onto general.yaml, without a
// Kubernetes version change.
//
// Only `bootstrap` re-renders values-capi-cluster.yaml today, so a machineType or replicas
// edit in general.yaml never reaches a provisioned cluster. This re-renders that one file,
// shows the operator the diff, and lets ArgoCD apply it.
//
// Nothing here deletes or recreates a MachineTemplate. ClusterAPI decides a Machine is out
// of date by comparing the *name* of the template it was cloned from against the name its
// owner references, so a template rewritten under its old name is invisible to it. The
// capi-cluster chart instead names HCloud MachineTemplates after a hash of their spec
// (global.machineTemplateRotation), which turns an instance-type change into a new template
// name and a normal rolling replacement — entirely through GitOps.
func SyncCAPICluster(ctx context.Context, args SyncCAPIClusterArgs) {
	bar := progress.New("Syncing cluster with general.yaml")
	defer bar.Finish()

	ctx = progress.WithBar(ctx, bar)

	// Resolve which live cluster we are about to reconcile, and make the operator say so.
	// This runs before the repo is even cloned : the whole command is a no-op if the answer
	// is "wrong cluster".
	bar.Describe("Confirming the target cluster")

	kubeconfigPath := resolveLiveClusterKubeconfig(ctx)

	err := confirmLiveClusterContext(bar, kubeconfigPath,
		"reconcile the cluster's ClusterAPI resources onto general.yaml",
	)
	assert.AssertErrNil(ctx, err, "Refusing to sync")

	bar.Substep(fmt.Sprintf("Targeting cluster %s", config.ParsedGeneralConfig.Cluster.Name))

	// Clone kubeaid-config, so we can re-render into it.
	bar.Describe("Re-rendering values-capi-cluster.yaml from general.yaml")

	gitAuthMethod := git.GetGitAuthMethod(ctx)

	repo := git.CloneRepo(ctx, config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, gitAuthMethod)
	bar.Substep("Cloned kubeaid-config repo")

	clusterDir := utils.GetClusterDir()

	capiValuesPath := path.Join(clusterDir,
		strings.TrimSuffix(constants.TemplateNameCapiClusterValues, ".tmpl"),
	)

	before, err := os.ReadFile(capiValuesPath)
	assert.AssertErrNil(ctx, err, "Failed reading the cluster's current values-capi-cluster.yaml")

	// Re-render values-capi-cluster.yaml, plus the verbatim general.yaml copy that sits
	// beside it, so the PR always carries the source of truth alongside what it produced.
	templateValues := getTemplateValues(ctx)
	createOrUpdateCapiClusterValuesFile(ctx, templateValues, clusterDir)
	createOrUpdateGeneralConfigFile(ctx, templateValues, clusterDir)

	after, err := os.ReadFile(capiValuesPath)
	assert.AssertErrNil(ctx, err, "Failed reading the re-rendered values-capi-cluster.yaml")

	diff, err := unifiedDiff("values-capi-cluster.yaml", before, after)
	assert.AssertErrNil(ctx, err, "Failed diffing values-capi-cluster.yaml")

	if len(diff) == 0 {
		bar.Substep("values-capi-cluster.yaml already matches general.yaml — nothing to sync")
		return
	}

	// Work out how ClusterAPI will react, and refuse the changes that would silently do
	// nothing or endanger etcd quorum.
	plan, err := planCapiValuesSync(before, after)
	assert.AssertErrNil(ctx, err, "Failed inspecting the values-capi-cluster.yaml change")

	if refusals := plan.Refusals(); len(refusals) > 0 {
		assert.Assert(ctx, false, strings.Join(refusals, "\n\n"))
	}

	// Approval #1 : the inline diff, before anything leaves this machine.
	if !confirmCapiValuesDiff(bar, diff, plan) {
		assert.Assert(ctx, false, "Aborted : declined the values-capi-cluster.yaml change")
	}

	// Approval #2 : merging the PR.
	bar.Describe("Pushing the change to kubeaid-config")

	if !pushCapiClusterValuesChanges(ctx, repo, gitAuthMethod, args.SkipPRWorkflow) {
		bar.Substep("kubeaid-config already up to date — nothing to sync")
		return
	}

	// Let ArgoCD apply the merged values, then watch ClusterAPI act on them.
	applyCapiClusterValues(ctx, plan)
}

// resolveLiveClusterKubeconfig points KUBECONFIG at whichever cluster currently holds the
// ClusterAPI resources : the main cluster once `clusterctl move` has run, the management
// cluster before that.
func resolveLiveClusterKubeconfig(ctx context.Context) string {
	utils.MustSetEnv(constants.EnvNameKubeconfig, constants.OutputPathMainClusterKubeconfig)

	if !kubernetes.IsClusterctlMoveExecuted(ctx) {
		mgmtKubeconfig, err := kubernetes.GetManagementClusterKubeconfigPath(ctx)
		assert.AssertErrNil(ctx, err, "Failed getting management cluster kubeconfig path")

		utils.MustSetEnv(constants.EnvNameKubeconfig, mgmtKubeconfig)
	}

	return utils.MustGetEnv(constants.EnvNameKubeconfig)
}

// unifiedDiff renders a `diff -u` style patch, or "" when the two versions are identical.
func unifiedDiff(name string, before, after []byte) (string, error) {
	if string(before) == string(after) {
		return "", nil
	}

	return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(before)),
		B:        difflib.SplitLines(string(after)),
		FromFile: name + " (live)",
		ToFile:   name + " (general.yaml)",
		Context:  3,
	})
}

// confirmCapiValuesDiff shows the operator exactly what will change, and what ClusterAPI
// will do about it, before a PR exists.
func confirmCapiValuesDiff(bar *progress.Bar, diff string, plan *capiValuesSyncPlan) bool {
	description := diff

	if reasons := plan.rollReasons(); len(reasons) > 0 {
		description += fmt.Sprintf(
			"\nThis rotates a MachineTemplate name, so ClusterAPI will REPLACE machines :\n\n%s\n\n"+
				"Each replacement adds a machine, waits for it to join, then removes an old one.\n"+
				"The control plane will roll with %d replica(s).",
			indentLines(reasons, "  - "), plan.ControlPlaneReplicasAfter,
		)
	}

	if len(plan.ReplicaChange) > 0 {
		description += fmt.Sprintf(
			"\nThe control plane scales : %s. The MachineTemplate spec is unchanged, so the new\n"+
				"members simply join etcd — no existing machine is replaced.",
			plan.ReplicaChange,
		)
	}

	for _, warning := range plan.Warnings() {
		description += "\n\nWARNING : " + warning
	}

	proceed := false

	bar.Pause()
	defer bar.Resume()

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("values-capi-cluster.yaml will change").
				Description(description),
			huh.NewConfirm().
				Title("Open a kubeaid-config PR with this change?").
				Affirmative("Yes, push it").
				Negative("No, abort").
				Value(&proceed),
		),
	).Run(); err != nil {
		return false
	}

	return proceed
}

// pushCapiClusterValuesChanges commits the re-rendered files and waits for the PR to merge.
// Returns false when the worktree was already clean.
func pushCapiClusterValuesChanges(ctx context.Context,
	repo *goGit.Repository,
	gitAuthMethod transport.AuthMethod,
	skipPRWorkflow bool,
) bool {
	bar := progress.FromCtx(ctx)

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting kubeaid-config repo worktree")

	defaultBranchName := git.GetDefaultBranchName(ctx, gitAuthMethod, repo)

	targetBranchName := defaultBranchName
	if !skipPRWorkflow {
		newBranchName := fmt.Sprintf("kubeaid-%s-%d",
			config.ParsedGeneralConfig.Cluster.Name, time.Now().Unix(),
		)
		git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, gitAuthMethod)

		targetBranchName = newBranchName
	}

	commitMessage := fmt.Sprintf("(cluster/%s) : synced values-capi-cluster.yaml with general.yaml",
		config.ParsedGeneralConfig.Cluster.Name,
	)

	commitHash := git.AddCommitAndPushChanges(ctx,
		repo, workTree, targetBranchName, gitAuthMethod,
		config.ParsedGeneralConfig.Cluster.Name, commitMessage, defaultBranchName,
	)
	if commitHash.IsZero() {
		return false
	}

	if !skipPRWorkflow {
		releasePRWait := bar.InProgress("Waiting for you to merge the PR")
		git.WaitUntilPRMerged(ctx, repo, defaultBranchName, commitHash, gitAuthMethod, targetBranchName)
		releasePRWait()

		bar.Substep("Confirmed PR merged")
	}

	return true
}

// applyCapiClusterValues syncs the capi-cluster ArgoCD App and waits for ClusterAPI to
// settle on the new values.
func applyCapiClusterValues(ctx context.Context, plan *capiValuesSyncPlan) {
	bar := progress.FromCtx(ctx)
	bar.Describe("Applying the change to the cluster")

	clusterClient, err := kubernetes.CreateKubernetesClient(ctx, utils.MustGetEnv(constants.EnvNameKubeconfig))
	assert.AssertErrNil(ctx, err, "Failed constructing Kubernetes cluster client")

	{
		argoCDClient, argoCDErr := kubernetes.NewArgoCDClient(ctx, clusterClient)
		assert.AssertErrNil(ctx, argoCDErr, "Failed creating ArgoCD client")

		globals.ArgoCDApplicationClientCloser, globals.ArgoCDApplicationClient = argoCDClient.NewApplicationClientOrDie()
		defer globals.ArgoCDApplicationClientCloser.Close()
	}

	// Sync the whole app rather than named resources : a rotating change creates a
	// differently-named MachineTemplate, so there is no stable resource list to scope to.
	err = kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, nil)
	assert.AssertErrNil(ctx, err, "Failed syncing the capi-cluster ArgoCD app")

	bar.Substep("Synced the capi-cluster ArgoCD app")

	// A pure scale-out is complete once the new control-plane machines are Running; a
	// rotating change additionally has to retire the old ones. Both are covered by waiting
	// for the KubeadmControlPlane to report every replica updated and no surge in flight.
	bar.Describe("Waiting for ClusterAPI to converge")

	err = kubernetes.WaitForControlPlaneRolloutComplete(ctx, clusterClient)
	assert.AssertErrNil(ctx, err, "Failed waiting for the control plane to converge")

	if plan.rolls() {
		bar.Substep("Control plane rolled onto the new MachineTemplate")
		return
	}

	bar.Substep(fmt.Sprintf("Control plane scaled to %d replica(s)", plan.ControlPlaneReplicasAfter))
}
