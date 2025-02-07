package core

import (
	"context"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
)

func CreateDevEnv(ctx context.Context, skipKubePrometheusBuild bool) {
	// Any cloud specific tasks.
	switch {
	case config.ParsedConfig.Cloud.AWS != nil:
		aws.SetAWSSpecificEnvs()
		aws.CreateIAMCloudFormationStack()
	}

	os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathManagementClusterKubeconfig)

	// Create the management cluster (using K3d), if it doesn't already exist.
	utils.CreateK3DCluster(ctx, "management-cluster")

	// Install Sealed Secrets.
	utils.InstallSealedSecrets(ctx)

	// Detect git authentication method.
	gitAuthMethod := utils.GetGitAuthMethod(ctx)

	// Clone the KubeAid config fork locally (if not already cloned).
	_ = utils.GitCloneRepo(ctx,
		config.ParsedConfig.Forks.KubeaidConfigForkURL,
		utils.GetKubeAidConfigDir(),
		gitAuthMethod,
	)

	// Setup cluster directory in the user's KubeAid config repo.
	SetupKubeAidConfig(ctx, gitAuthMethod, skipKubePrometheusBuild)

	managementClusterClient, _ := utils.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig, true)

	// Setup the management cluster.
	SetupCluster(ctx, managementClusterClient)
}
