package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/urfave/cli/v2"
)

func DeleteCluster(ctx *cli.Context) error {
	// Parse the config file.
	configFilePath := ctx.Path(constants.FlagNameConfigFile)
	constants.ParsedConfig = config.ParseConfigFile(configFilePath)

	// Set the KUBECONFIG environment variable's value to the management cluster's kubeconfig.
	os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathManagementClusterKubeconfig)

	var (
		capiClusterNamespace = utils.GetCapiClusterNamespace()
		clusterName          = constants.ParsedConfig.Cluster.ClusterName
	)

	// Detects whether 'clusterctl move' has been executed or not, in the management cluster.
	output := utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl get cluster -n %s", capiClusterNamespace))
	isClusterctlMoveExecuted := strings.Contains(output, clusterName)

	if isClusterctlMoveExecuted {
		// Move back the ClusterAPI manifests back from the provisioned cluster to the management
		// cluster.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl move --kubeconfig %s --to-kubeconfig %s -n %s",
			constants.OutputPathProvisionedClusterKubeconfig,
			constants.OutputPathManagementClusterKubeconfig,
			capiClusterNamespace,
		))
	}

	utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl delete cluster/%s -n %s", clusterName, capiClusterNamespace))

	return nil
}
