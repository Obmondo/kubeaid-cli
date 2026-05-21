// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	gitUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes/k3d"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type CreateDevEnvArgs struct {
	ManagementClusterName string

	SkipMonitoringSetup,
	SkipPRWorkflow,

	IsPartOfDisasterRecovery bool
}

func CreateDevEnv(ctx context.Context, args *CreateDevEnvArgs) {
	bar := progress.FromCtx(ctx)

	// Detect git authentication method.
	gitAuthMethod := gitUtils.GetGitAuthMethod(ctx)

	// Any cloud specific tasks.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		err := aws.SetAWSSpecificEnvs(ctx)
		assert.AssertErrNil(ctx, err, "Failed setting AWS specific environment variables")

		err = aws.CreateIAMCloudFormationStack(ctx)
		assert.AssertErrNil(ctx, err, "Failed creating IAM CloudFormation stack")
	}

	// Ensure that the KubeAid Config repo is cloned locally.
	_ = gitUtils.CloneRepo(ctx, config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, gitAuthMethod)
	bar.Substep("Cloned kubeaid-config repo")

	// When using the Bare Metal provider, no management cluster is needed.
	// We just need to create the KubeOne config file.
	if globals.CloudProviderName == constants.CloudProviderBareMetal {
		SetupKubeAidConfig(ctx, SetupKubeAidConfigArgs{
			CreateDevEnvArgs: args,
			GitAuthMethod:    gitAuthMethod,
		})

		return
	}

	// Set KUBECONFIG env and create the K3D management cluster.
	managementClusterKubeconfigPath, err := kubernetes.GetManagementClusterKubeconfigPath(ctx)
	assert.AssertErrNil(ctx, err, "Failed getting management cluster kubeconfig path")
	utils.MustSetEnv(constants.EnvNameKubeconfig, managementClusterKubeconfigPath)

	// Ensure that the K3D management cluster is created.
	clusterName := resolveManagementClusterName(args.ManagementClusterName)
	err = k3d.CreateK3DCluster(ctx, clusterName)
	assert.AssertErrNil(ctx, err, "Failed creating K3D cluster")
	bar.Substep("Created k3d management cluster")

	managementClusterClient, err := kubernetes.CreateKubernetesClient(ctx, managementClusterKubeconfigPath)
	assert.AssertErrNil(ctx, err, "Failed constructing Kubernetes cluster client")

	// Setup the management cluster. SetupCluster emits its own
	// substeps (Sealed Secrets install, kubeaid-config render +
	// PR-merge confirmation, ArgoCD install, per-app sync) so this
	// outer block doesn't need a single rolled-up "Installed
	// ArgoCD + sealed-secrets" trailer — the granular substeps tell
	// the operator exactly which long-running step is in flight.
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs: args,
		ClusterType:      constants.ClusterTypeManagement,
		ClusterClient:    managementClusterClient,
		GitAuthMethod:    gitAuthMethod,
	})
}

// resolveManagementClusterName returns the name to use for the local k3d bootstrap cluster.
//
// When the operator supplies --management-cluster-name explicitly, that value is used unchanged.
// When the flag is omitted (empty string), the name is derived from the target cluster name in
// general.yaml, scoping the k3d cluster to this bootstrap target. This prevents a second run —
// or a bootstrap of a different target cluster — from silently reusing a stale k3d cluster
// carrying the previous run's Cluster API state, Sealed Secrets keys, and ArgoCD registrations.
func resolveManagementClusterName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return fmt.Sprintf("%s%s",
		constants.ManagementClusterNamePrefix,
		config.ParsedGeneralConfig.Cluster.Name,
	)
}
