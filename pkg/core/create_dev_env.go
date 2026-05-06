// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes/k3d"
)

type CreateDevEnvArgs struct {
	ManagementClusterName string

	SkipMonitoringSetup,
	SkipPRWorkflow,

	IsPartOfDisasterRecovery bool
}

func CreateDevEnv(ctx context.Context, args *CreateDevEnvArgs) {
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
	err = k3d.CreateK3DCluster(ctx, args.ManagementClusterName)
	assert.AssertErrNil(ctx, err, "Failed creating K3D cluster")

	managementClusterClient, err := kubernetes.CreateKubernetesClient(ctx, managementClusterKubeconfigPath)
	assert.AssertErrNil(ctx, err, "Failed constructing Kubernetes cluster client")

	// Setup the management cluster.
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs: args,
		ClusterType:      constants.ClusterTypeManagement,
		ClusterClient:    managementClusterClient,
		GitAuthMethod:    gitAuthMethod,
	})
}
