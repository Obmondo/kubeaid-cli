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

	// Set KUBECONFIG env.
	managementClusterKubeconfigPath := kubernetes.GetManagementClusterKubeconfigPath(ctx)
	utils.MustSetEnv(constants.EnvNameKubeconfig, managementClusterKubeconfigPath)
	//
	// and then create the K3D management cluster (if it doesn't already exist).
	k3d.CreateK3DCluster(ctx, args.ManagementClusterName)

	// Clone the KubeAid config fork locally (if not already cloned).
	_ = gitUtils.CloneRepo(ctx,
		config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL,
		utils.GetKubeAidConfigDir(),
		gitAuthMethod,
	)

	managementClusterClient := kubernetes.MustCreateClusterClient(ctx,
		managementClusterKubeconfigPath,
	)

	// Setup the management cluster.
	SetupCluster(ctx, SetupClusterArgs{
		CreateDevEnvArgs: args,
		ClusterType:      constants.ClusterTypeManagement,
		ClusterClient:    managementClusterClient,
		GitAuthMethod:    gitAuthMethod,
	})
}
