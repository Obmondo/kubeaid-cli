package core

import (
	"context"
	"errors"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
)

func CreateDevEnv(ctx context.Context,
	managementClusterName string,
	skipKubePrometheusBuild,
	isPartOfDisasterRecovery bool,
) {
	// Detect git authentication method.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

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

	// Set KUBECONFIG env.
	os.Setenv(constants.EnvNameKubeconfig, getManagementClusterKubeconfigPath(ctx))
	//
	// and then create the K3D management cluster (if it doesn't already exist).
	kubernetes.CreateK3DCluster(ctx, managementClusterName)

	// Clone the KubeAid config fork locally (if not already cloned).
	_ = git.CloneRepo(ctx,
		config.ParsedConfig.Forks.KubeaidConfigForkURL,
		utils.GetKubeAidConfigDir(),
		gitAuthMethod,
	)

	managementClusterClient, _ := kubernetes.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterContainerKubeconfig, true)

	// Setup the management cluster.
	SetupCluster(ctx,
		managementClusterClient,
		gitAuthMethod,
		skipKubePrometheusBuild,
		isPartOfDisasterRecovery,
	)
}

// Returns the management cluster kubeconfig file path, based on whether the script is running
// inside a container or not.
func getManagementClusterKubeconfigPath(ctx context.Context) string {
	if amContainerized(ctx) {
		return constants.OutputPathManagementClusterContainerKubeconfig
	}

	return constants.OutputPathManagementClusterHostKubeconfig
}

// Detetcs whether the KubeAid Bootstrap Script is running inside a container or not.
// If the /.dockerenv file exists, then that means, it's running inside a container.
// Only compatible with the Docker container engine for now.
func amContainerized(ctx context.Context) bool {
	_, err := os.Stat("/.dockerenv")
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	assert.AssertErrNil(ctx, err, "Failed detecting whether running inside a container or not")
	return true
}
