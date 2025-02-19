package core

import (
	"context"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

func CreateDevEnv(ctx context.Context, skipKubePrometheusBuild bool) {
	// Any cloud specific tasks.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		aws.SetAWSSpecificEnvs()
		aws.CreateIAMCloudFormationStack()

	case constants.CloudProviderAzure:
		panic("unimplemented")

	case constants.CloudProviderHetzner:
		break
	}

	os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathManagementClusterKubeconfig)

	// Create the management cluster (using K3d), if it doesn't already exist.
	kubernetes.CreateK3DCluster(ctx, "management-cluster")

	// Install Sealed Secrets.
	kubernetes.InstallSealedSecrets(ctx)

	// Detect git authentication method.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

	// Clone the KubeAid config fork locally (if not already cloned).
	_ = git.CloneRepo(ctx,
		config.ParsedConfig.Forks.KubeaidConfigForkURL,
		utils.GetKubeAidConfigDir(),
		gitAuthMethod,
	)

	// Setup cluster directory in the user's KubeAid config repo.
	SetupKubeAidConfig(ctx, gitAuthMethod, skipKubePrometheusBuild)

	managementClusterClient, _ := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig, true)

	// Setup the management cluster.
	SetupCluster(ctx, managementClusterClient)
}
