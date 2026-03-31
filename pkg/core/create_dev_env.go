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
		aws.SetAWSSpecificEnvs(ctx)
		aws.CreateIAMCloudFormationStack(ctx)
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
	managementClusterKubeconfigPath := kubernetes.GetManagementClusterKubeconfigPath(ctx)
	utils.MustSetEnv(constants.EnvNameKubeconfig, managementClusterKubeconfigPath)

	// Ensure that the K3D management cluster is created.
	k3d.CreateK3DCluster(ctx, args.ManagementClusterName)

	managementClusterClient := kubernetes.MustCreateClusterClient(ctx, managementClusterKubeconfigPath)

	// Setup the management cluster.
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs: args,
		ClusterType:      constants.ClusterTypeManagement,
		ClusterClient:    managementClusterClient,
		GitAuthMethod:    gitAuthMethod,
	})
}
