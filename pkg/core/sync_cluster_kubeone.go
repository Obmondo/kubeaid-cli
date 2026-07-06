// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
)

type SyncKubeOneClusterArgs struct {
	SkipPRWorkflow bool
}

// SyncClusterUsingKubeOne reconciles a Bare Metal (KubeOne provisioned) cluster with
// general.yaml, without a Kubernetes version change (that's 'cluster upgrade's job) :
// re-renders the KubeOne manifest, pushes it (PR workflow unless skipped) and runs a plain
// 'kubeone apply'. KubeOne's steady-state task set reconciles the bundled helm releases and
// addons, joins newly added static workers, renews soon-to-expire certificates and re-labels
// nodes - it never cordons / drains in-version nodes, so sync is non-disruptive and safe to
// rerun anytime.
//
// Deliberately NOT covered : kubelet tuning (cloud.bare-metal.kubelet). KubeOne rewrites the
// kubelet flags on a host only during its per-node upgrade procedure (cordon + drain +
// restart), which sync never forces. Those changes take effect on the next 'cluster upgrade'.
func SyncClusterUsingKubeOne(ctx context.Context, args SyncKubeOneClusterArgs) {
	targetVersion := config.ParsedGeneralConfig.Cluster.K8sVersion

	gitAuthMethod := git.GetGitAuthMethod(ctx)
	repo := git.CloneRepo(ctx, config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, gitAuthMethod)

	// (1) Pre-flight.

	currentVersion, fromLiveCluster := getCurrentBareMetalClusterK8sVersion(ctx)

	err := validateK8sVersionHop(currentVersion, targetVersion)
	switch {
	case errors.Is(err, errK8sVersionAlreadyAtTarget):
		// The cluster and general.yaml agree on the version - exactly what sync expects.

	case err == nil:
		assert.Assert(ctx, false, fmt.Sprintf(
			"The cluster runs Kubernetes %s while general.yaml declares %s - run 'kubeaid-cli cluster upgrade' first. 'cluster sync' only reconciles non-version changes",
			currentVersion, targetVersion,
		))

	default:
		assert.AssertErrNil(ctx, err, "Rejecting cluster.k8sVersion in general.yaml")
	}

	if fromLiveCluster {
		assertAllNodesReady(ctx)
	}

	if config.ParsedGeneralConfig.Cloud.BareMetal.Kubelet != nil {
		slog.InfoContext(
			ctx,
			"Note : kubelet tuning (cloud.bare-metal.kubelet) is applied by KubeOne only during node upgrades - it takes effect on the next 'kubeaid-cli cluster upgrade', not on sync",
		)
	}

	// (2) Re-render the KubeOne manifest from general.yaml and push it to the KubeAid Config
	//     repository.

	commitMessage := fmt.Sprintf(
		"(cluster/%s) : syncing cluster config",
		config.ParsedGeneralConfig.Cluster.Name,
	)
	manifestChanged := pushKubeOneManifestChanges(
		ctx, repo, gitAuthMethod, commitMessage, args.SkipPRWorkflow,
	)
	if !manifestChanged {
		slog.InfoContext(
			ctx,
			"KubeOne manifest is unchanged - running 'kubeone apply' anyway : it reconciles helm releases / addons and renews soon-to-expire certificates, and a previous sync may have died before reaching it",
		)
	}

	// (3) Run a plain 'kubeone apply' against the manifest.

	assertControlPlaneHostsNotHalfInitialized(ctx)
	assertBareMetalHostsPackageStateHealthy(ctx)

	applyKubeOneManifest(ctx, "sync")

	// (4) Wait until every node is Ready.

	waitForNodesAtKubeletVersion(ctx, targetVersion)

	slog.InfoContext(ctx, "Cluster is in sync with general.yaml 🎉")
}
