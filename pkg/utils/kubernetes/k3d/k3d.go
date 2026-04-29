// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package k3d

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/k3d-io/k3d/v5/cmd/cluster"
	k3dClient "github.com/k3d-io/k3d/v5/pkg/client"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3dTypes "github.com/k3d-io/k3d/v5/pkg/types"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	templateUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

//go:embed templates/*
var templates embed.FS

type (
	K3DConfigTemplateValues struct {
		Name,
		K8sVersion,
		K3sVersion string

		WorkloadIdentity *WorkloadIdentity
	}

	WorkloadIdentity struct {
		ServiceAccountIssuerURL,

		SSHPublicKeyFilePath,
		SSHPrivateKeyFilePath string
	}
)

type K3DRuntime interface {
	ClusterList(ctx context.Context) ([]*k3dTypes.Cluster, error)
	ClusterCreate(configPath string) error
	ClusterDelete(configPath string) error
	WriteKubeconfig(ctx context.Context, clusterName, outputPath string) error
}

type dockerK3DRuntime struct{}

func (dockerK3DRuntime) ClusterList(ctx context.Context) ([]*k3dTypes.Cluster, error) {
	return k3dClient.ClusterList(ctx, runtimes.Docker)
}

func (dockerK3DRuntime) ClusterCreate(configPath string) error {
	cmd := cluster.NewCmdClusterCreate()
	cmd.SetArgs([]string{"--config", configPath})

	// k3d's cluster create command prints help text (e.g. "kubectl cluster-info")
	// directly to stdout via fmt.Println, bypassing logrus. Temporarily redirect
	// stdout to suppress this output.
	origStdout := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	err := cmd.Execute()
	os.Stdout = origStdout
	return err
}

func (dockerK3DRuntime) ClusterDelete(configPath string) error {
	cmd := cluster.NewCmdClusterDelete()
	cmd.SetArgs([]string{"--config", configPath})
	return cmd.Execute()
}

func (dockerK3DRuntime) WriteKubeconfig(ctx context.Context, clusterName, outputPath string) error {
	_, err := k3dClient.KubeconfigGetWrite(ctx, runtimes.Docker,
		&k3dTypes.Cluster{Name: clusterName},
		outputPath,
		&k3dClient.WriteKubeConfigOptions{OverwriteExisting: true},
	)
	return err
}

// DockerRuntime is the singleton production K3DRuntime.
var DockerRuntime K3DRuntime = dockerK3DRuntime{}

type createK3DClusterParams struct {
	Runtime                 K3DRuntime
	Executor                commandexecutor.CommandExecutor
	ConfigPath              string
	HostKubeconfigPath      string
	ContainerKubeconfigPath string
}

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
func CreateK3DCluster(ctx context.Context, name string) error {
	return createK3DClusterWithParams(ctx, name, &createK3DClusterParams{
		Runtime:                 DockerRuntime,
		Executor:                commandexecutor.NewLocalCommandExecutor(false),
		ConfigPath:              constants.OutputPathManagementClusterK3DConfig,
		HostKubeconfigPath:      constants.OutputPathManagementClusterHostKubeconfig,
		ContainerKubeconfigPath: constants.OutputPathManagementClusterContainerKubeconfig,
	})
}

func createK3DClusterWithParams(ctx context.Context, name string, params *createK3DClusterParams) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("cluster-name", name),
	})

	// Generate the K3D cluster config file.
	if err := generateK3DClusterConfigFile(ctx, name, params.ConfigPath); err != nil {
		return fmt.Errorf("generating k3d cluster config file: %w", err)
	}

	// Ensure that the K3D cluster is created.
	if err := createK3DCluster(ctx, name, params.ConfigPath, params.Runtime); err != nil {
		return fmt.Errorf("creating k3d cluster: %w", err)
	}

	// Ensure existence of the directory which'll contain the kubeconfig file.
	utils.CreateIntermediateDirsForFile(ctx, params.HostKubeconfigPath)

	// Create the K3D management cluster's host kubeconfig.
	// Use https://0.0.0.0:<whatever the random port is> as the API server address.
	if err := params.Runtime.WriteKubeconfig(ctx, name, params.HostKubeconfigPath); err != nil {
		return fmt.Errorf("writing k3d cluster kubeconfig: %w", err)
	}

	// For management cluster's in-container kubeconfig, use
	// https://k3d-management-cluster-server-0:6443 as the API server address.
	params.Executor.MustExecute(ctx,
		fmt.Sprintf(
			`
        cp %s %s
        KUBECONFIG=%s kubectl config set-cluster k3d-%s --server=https://k3d-%s-server-0:6443
      `,
			params.HostKubeconfigPath,
			params.ContainerKubeconfigPath,
			params.ContainerKubeconfigPath,
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
	params.Executor.MustExecute(ctx, `
		master_nodes=$(kubectl get nodes -l node-role.kubernetes.io/control-plane=true -o name)

		for node in $master_nodes; do
			kubectl label $node node-role.kubernetes.io/control-plane-
			kubectl label $node node-role.kubernetes.io/control-plane=""
		done
	`)

	return nil
}

func generateK3DClusterConfigFile(ctx context.Context, clusterName, configPath string) error {
	k3sVersion, err := getK3sVersion()
	if err != nil {
		return fmt.Errorf("getting k3s version: %w", err)
	}

	k3dConfigTemplateValues := &K3DConfigTemplateValues{
		Name:       clusterName,
		K3sVersion: k3sVersion,
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

	k3dConfigFile, err := os.OpenFile(configPath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600,
	)
	if err != nil {
		return fmt.Errorf("opening k3d config file %s: %w", configPath, err)
	}
	defer k3dConfigFile.Close()

	if _, err = k3dConfigFile.Write(k3dConfigAsBytes); err != nil {
		return fmt.Errorf("writing k3d config to file: %w", err)
	}

	return nil
}

// createK3DCluster creates the K3D cluster if it doesn't already exist.
func createK3DCluster(ctx context.Context, name, configPath string, rt K3DRuntime) error {
	exists, err := doesK3dClusterExist(ctx, name, rt)
	if err != nil {
		return fmt.Errorf("checking if k3d cluster exists: %w", err)
	}

	if exists {
		slog.InfoContext(ctx, "Skipped creating the K3D management cluster")
		return nil
	}

	slog.InfoContext(ctx, "Creating the K3D management cluster")

	if err := rt.ClusterCreate(configPath); err != nil {
		return fmt.Errorf("creating k3d cluster: %w", err)
	}

	return nil
}

// doesK3dClusterExist returns whether the given K3D cluster exists.
func doesK3dClusterExist(ctx context.Context, name string, rt K3DRuntime) (bool, error) {
	clusters, err := rt.ClusterList(ctx)
	if err != nil {
		return false, fmt.Errorf("listing k3d clusters: %w", err)
	}

	for _, c := range clusters {
		if c.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// DeleteK3DCluster deletes the K3D management cluster.
func DeleteK3DCluster(ctx context.Context) error {
	return deleteK3DClusterWithRuntime(ctx, constants.OutputPathManagementClusterK3DConfig, DockerRuntime)
}

func deleteK3DClusterWithRuntime(ctx context.Context, configPath string, rt K3DRuntime) error {
	slog.InfoContext(ctx, "Deleting the K3D management cluster")

	if err := rt.ClusterDelete(configPath); err != nil {
		return fmt.Errorf("deleting k3d cluster: %w", err)
	}

	return nil
}

// Returns K3s version to be used for the cluster being spunup using K3D.
func getK3sVersion() (string, error) {
	// As you know : for the Local provider, we spinup a local K3s cluster, where the user can try
	// out KubeAid.
	// Just use the K8s version specified in the general.yaml file.
	if globals.CloudProviderName == constants.CloudProviderLocal {
		return fmt.Sprintf("%s-k3s1", config.ParsedGeneralConfig.Cluster.K8sVersion), nil
	}

	// Otherwise, just use the latest K3s version.
	return getLatestK3sVersion()
}

// GetMaxK3sSupportedK8sVersion returns the max K8s version supported by K3s.
func GetMaxK3sSupportedK8sVersion(ctx context.Context) (string, error) {
	latestK3sVersion, err := getLatestK3sVersion()
	if err != nil {
		return "", fmt.Errorf("getting latest k3s version: %w", err)
	}

	// Extract the corresponding K8s version (before '-k3s').
	i := strings.Index(latestK3sVersion, "-")
	if i <= 0 {
		return "", fmt.Errorf("extracting k8s version from k3s version %q: no '-' separator found", latestK3sVersion)
	}

	return latestK3sVersion[:i], nil
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

var k3sReleasesURL = constants.K3sReleasesAPIURL

// getLatestK3sVersion returns the latest K3s version.
func getLatestK3sVersion() (string, error) {
	response, err := http.Get(k3sReleasesURL)
	if err != nil {
		return "", fmt.Errorf("fetching latest k3s release: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return "", fmt.Errorf("fetching latest k3s release: unexpected status %d", response.StatusCode)
	}
	defer response.Body.Close()

	var release GitHubRelease
	if err = json.NewDecoder(response.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decoding k3s release JSON: %w", err)
	}

	return strings.ReplaceAll(release.TagName, "+", "-"), nil
}
