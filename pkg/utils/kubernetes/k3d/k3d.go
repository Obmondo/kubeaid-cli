package k3d

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"

	"github.com/k3d-io/k3d/v5/cmd/cluster"
	k3dClient "github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	"github.com/k3d-io/k3d/v5/pkg/types"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	templateUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

//go:embed templates/*
var templates embed.FS

type (
	K3DConfigTemplateValues struct {
		Name,
		K8sVersion string

		WorkloadIdentity *WorkloadIdentity
	}

	WorkloadIdentity struct {
		ServiceAccountIssuerURL,

		SSHPublicKeyFilePath,
		SSHPrivateKeyFilePath string
	}
)

/*
Does the following :

	(1) Creates a K3D cluster with the given name, if it doesn't already exist.

	(2) Creates 2 kubeconfig files, which can be used to access the cluster, from inside the
	    KubeAid Bootstrap Script container, or from the user's host machine.

	(3) Ensures that each master node has the node-role.kubernetes.io/control-plane= label,
	    just like it is for a Vanilla Kubernetes cluster.

Keep in mind :

	The created K3D cluster and the KubeAid core container, must be running in the same network.
	Otherwise, access to the K3D cluster will break.

	(1) From inside the container, we can access the K3D cluster's API server using
	    https://k3d-management-cluster-server-0:6443.

	(2) And from outside the container, we can use https://0.0.0.0:<whatever the random port is>.
*/
func CreateK3DCluster(ctx context.Context, name string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("cluster-name", name),
	})

	// Generate the K3D cluster config file.
	generateK3DClusterConfigFile(ctx, name)

	// Create the K3D cluster, if it doesn't already exist.
	createK3dCluster(ctx, name)

	// Create the K3D management cluster's host kubeconfig.
	// Use https://0.0.0.0:<whatever the random port is> as the API server address.
	_, err := k3dClient.KubeconfigGetWrite(ctx, runtimes.Docker,
		&types.Cluster{Name: name},

		constants.OutputPathManagementClusterHostKubeconfig,
		&k3dClient.WriteKubeConfigOptions{OverwriteExisting: true},
	)
	assert.AssertErrNil(ctx, err, "Failed getting and persisting K3D cluster's kubeconfig")

	// For management cluster's in-container kubeconfig, use
	// https://k3d-management-cluster-server-0:6443 as the API server address.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"cp %s %s && KUBECONFIG=%s kubectl config set-cluster k3d-%s --server=https://k3d-%s-server-0:6443",
		constants.OutputPathManagementClusterHostKubeconfig,
		constants.OutputPathManagementClusterContainerKubeconfig,
		constants.OutputPathManagementClusterContainerKubeconfig,
		name,
		name,
	))

	/*
		Initially the master nodes have label node-role.kubernetes.io/control-plane=true.

		We'll remove that (using - at the end of the label key) and then update the value to ""
		(just like it is, in Vanilla Kubernetes). Some ArgoCD Apps (like capi-cluster) rely
		on this label to get scheduled to the master node.

		NOTE : Using options.k3s.nodeLabels to set that label for the control-plane nodes doesn't work.
		       The cluster won't even startup.
	*/
	utils.ExecuteCommandOrDie(`
		master_nodes=$(kubectl get nodes -l node-role.kubernetes.io/control-plane=true -o name)

		for node in $master_nodes; do
			kubectl label $node node-role.kubernetes.io/control-plane-
			kubectl label $node node-role.kubernetes.io/control-plane=""
		done
	`)
}

// Generates the K3D cluster config file.
func generateK3DClusterConfigFile(ctx context.Context, clusterName string) {
	k3dConfigTemplateValues := &K3DConfigTemplateValues{
		Name:       clusterName,
		K8sVersion: config.ParsedGeneralConfig.Cluster.K8sVersion,
	}
	if globals.CloudProviderName == constants.CloudProviderAzure {
		workloadIdentityConfig := config.ParsedGeneralConfig.Cloud.Azure.WorkloadIdentity

		k3dConfigTemplateValues.WorkloadIdentity = &WorkloadIdentity{
			ServiceAccountIssuerURL: azure.GetServiceAccountIssuerURL(ctx),

			SSHPublicKeyFilePath: utils.ToAbsolutePath(ctx,
				workloadIdentityConfig.OpenIDProviderSSHKeyPair.PublicKeyFilePath,
			),
			SSHPrivateKeyFilePath: utils.ToAbsolutePath(ctx,
				workloadIdentityConfig.OpenIDProviderSSHKeyPair.PrivateKeyFilePath,
			),
		}
	}

	k3dConfigAsBytes := templateUtils.ParseAndExecuteTemplate(ctx,
		&templates, constants.TemplateNameK3DConfig, k3dConfigTemplateValues,
	)

	k3dConfigFile, err := os.OpenFile(constants.OutputPathManagementClusterK3DConfig,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm,
	)
	assert.AssertErrNil(ctx, err,
		"Failed opening management cluster K3D config file",
		slog.String("path", constants.OutputPathManagementClusterK3DConfig),
	)
	defer k3dConfigFile.Close()

	_, err = k3dConfigFile.Write(k3dConfigAsBytes)
	assert.AssertErrNil(ctx, err, "Failed writing K3D config to file")
}

// Create the K3D cluster, if it doesn't already exist.
func createK3dCluster(ctx context.Context, name string) {
	if doesK3dClusterExist(ctx, name) {
		slog.InfoContext(ctx, "Skipped creating the K3D management cluster")
		return
	}

	slog.InfoContext(ctx, "Creating the K3D management cluster")

	clusterCreateCmd := cluster.NewCmdClusterCreate()
	clusterCreateCmd.SetArgs([]string{
		"--config",
		constants.OutputPathManagementClusterK3DConfig,
	})
	err := clusterCreateCmd.ExecuteContext(ctx)
	assert.AssertErrNil(ctx, err, "Failed creating K3D cluster")
}

// Returns whether the given K3D cluster exists or not.
func doesK3dClusterExist(ctx context.Context, name string) bool {
	clusters, err := k3dClient.ClusterList(ctx, runtimes.Docker)
	assert.AssertErrNil(ctx, err, "Failed listing K3d clusters")

	for _, cluster := range clusters {
		if cluster.Name == name {
			return true
		}
	}
	return false
}

func DeleteK3DCluster(ctx context.Context) {
	slog.InfoContext(ctx, "Deleting the K3D management cluster")

	clusterDeleteCmd := cluster.NewCmdClusterDelete()
	clusterDeleteCmd.SetArgs([]string{
		"--config",
		constants.OutputPathManagementClusterK3DConfig,
	})
	err := clusterDeleteCmd.ExecuteContext(ctx)
	assert.AssertErrNil(ctx, err, "Failed deleting K3D cluster")
}
