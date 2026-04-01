// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	validatorV10 "github.com/go-playground/validator/v10"
	goNonStandardValidators "github.com/go-playground/validator/v10/non-standard/validators"
	labelsPkg "github.com/siderolabs/talos/pkg/machinery/labels"
	"golang.org/x/crypto/ssh"
	"k8c.io/kubeone/pkg/executor"
	kubeonessh "k8c.io/kubeone/pkg/ssh"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes/k3d"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Validates the parsed general and secrets config.
func validateConfigs(ctx context.Context) error {
	if strings.Contains(config.ParsedGeneralConfig.Cluster.Name, ".") {
		return fmt.Errorf("cluster name cannot contain any dots")
	}

	// Cluster type VPN is supported only for the Hetzner provider as of now.
	if config.ParsedGeneralConfig.Cluster.Type == constants.ClusterTypeVPN {
		assert.Assert(ctx, (globals.CloudProviderName == constants.CloudProviderHetzner),
			"Cluster type VPN is supported only for the Hetzner provider as of now")
	}

	// Validate based on struct tags.

	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	err := validator.RegisterValidation("notblank", goNonStandardValidators.NotBlank)
	assert.AssertErrNil(ctx, err, "Failed registering notblank validator")

	err = validator.Struct(config.ParsedGeneralConfig)
	assert.AssertErrNil(ctx, err, "Struct validation failed for general config")

	err = validator.Struct(config.ParsedSecretsConfig)
	assert.AssertErrNil(ctx, err, "Struct validation failed for secrets config")

	// Validate K8s version.
	validateK8sVersion(ctx, config.ParsedGeneralConfig.Cluster.K8sVersion)

	// Validate KubePrometheus version.
	validateKubePrometheusVersion(ctx,
		config.ParsedGeneralConfig.KubePrometheus.Version,
		config.ParsedGeneralConfig.Cluster.K8sVersion,
	)

	// Validate additional users.
	for _, additionalUser := range config.ParsedGeneralConfig.Cluster.AdditionalUsers {
		// Additional user name cannot be ubuntu.
		assert.Assert(ctx, additionalUser.Name != "ubuntu", "Additional user name cannot be ubuntu")

		// Validate the public SSH key.
		_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(additionalUser.SSHPublicKey))
		assert.AssertErrNil(ctx, err,
			"SSH public key is invalid : failed parsing",
			slog.String("additional-user", additionalUser.Name),
		)
	}

	// Validate provider specific configurations.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		validateAWSConfig(ctx)

	case constants.CloudProviderAzure:
		validateAzureConfig(ctx)

	case constants.CloudProviderHetzner:
		validateHetznerConfig(ctx)

	case constants.CloudProviderBareMetal:
		validateBareMetalConfig(ctx)

	case constants.CloudProviderLocal:
		break
	}

	return nil
}

// Checks whether the given string represents a valid and supported Kubernetes version.
func validateK8sVersion(ctx context.Context, k8sVersion string) {
	hasPrefixV := strings.HasPrefix(k8sVersion, "v")
	assert.Assert(ctx, hasPrefixV, "K8s version must start with 'v' (for e.g.: v1.35.0)")

	// Determine the min and max K8s versions supported by KubeAid CLI,
	// considering the provider being used.

	var minSupportedK8sVersion,
		maxSupportedK8sVersion string

	switch globals.CloudProviderName {
	// For the Bare Metal provider, we use KubeOne under the hood.
	// And we need to ensure that the provided K8s version is officially supported by KubeOne.
	case constants.CloudProviderBareMetal:
		minSupportedK8sVersion = constants.MinKubeOneSupportedK8sVersion
		maxSupportedK8sVersion = constants.MaxKubeOneSupportedK8sVersion

	// When using the Local provider, we create a K3s cluster, where the user can try out KubeAid.
	// Ensure that the given K8s version is supported by K3s,
	// and compatible with the CGroup version on the host system.
	case constants.CloudProviderLocal:
		minSupportedK8sVersion = constants.MinSupportedK8sVersion

		maxSupportedK8sVersion = k3d.GetMaxK3sSupportedK8sVersion(ctx)
		if utils.IsCGroupV2() {
			maxSupportedK8sVersion = constants.MaxCGroupV1CompatibleK8sVersion
		}

	default:
		minSupportedK8sVersion = constants.MinSupportedK8sVersion
		maxSupportedK8sVersion = getLatestStableK8sVersion(ctx)
	}

	parsedMinSupportedK8sVersion, err := version.ParseMajorMinor(minSupportedK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing min supported K8s version")

	parsedMaxSupportedK8sVersion, err := version.ParseMajorMinor(maxSupportedK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing max supported K8s version")

	// Check that : min supported K8s version <= provided K8s version <= max supported K8s version.

	parsedK8sVersion, err := version.ParseSemantic(k8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing K8s semantic version")

	k8sVersionSupported := parsedK8sVersion.AtLeast(parsedMinSupportedK8sVersion) &&
		(parsedK8sVersion.LessThan(parsedMaxSupportedK8sVersion) || parsedK8sVersion.EqualTo(parsedMaxSupportedK8sVersion))
	assert.Assert(ctx, k8sVersionSupported,
		fmt.Sprintf("K8s version must be in the range (inclusive) : %s - %s",
			minSupportedK8sVersion, maxSupportedK8sVersion,
		),
	)
}

// Fetches and returns the latest stable Kubernetes version, from the Kubeadm API endpoint.
func getLatestStableK8sVersion(ctx context.Context) string {
	slog.InfoContext(
		ctx,
		"Fetching latest stable K8s version",
		slog.String("URL", constants.K8sReleaseAPIURL),
	)

	response, err := http.Get(constants.K8sReleaseAPIURL)
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed fetching latest stable Kubernetes version",
		logger.Error(err), slog.Any("response", response),
	)
	defer response.Body.Close()

	latestStableK8sVersion, err := io.ReadAll(response.Body)
	assert.AssertErrNil(ctx, err, "Failed reading latest stable K8s version from response body")

	return string(latestStableK8sVersion)
}

func validateAWSConfig(ctx context.Context) {
	// Ensure that the user has provided AWS specific credentials.
	assert.AssertNotNil(ctx, config.ParsedSecretsConfig.AWS, "AWS credentials not provided")

	awsConfig := config.ParsedGeneralConfig.Cloud.AWS

	// Validate auto-scalable node-groups.
	for _, awsAutoScalableNodeGroup := range awsConfig.NodeGroups {
		validateAutoScalableNodeGroup(ctx, &awsAutoScalableNodeGroup.AutoScalableNodeGroup)
	}
}

func validateAzureConfig(ctx context.Context) {
	// Ensure that the user has provided Azure specific credentials.
	assert.AssertNotNil(ctx, config.ParsedSecretsConfig.Azure, "Azure credentials not provided")

	azureConfig := config.ParsedGeneralConfig.Cloud.Azure

	// Validate auto-scalable node-groups.
	for _, azureAutoScalableNodeGroup := range azureConfig.NodeGroups {
		validateAutoScalableNodeGroup(ctx, &azureAutoScalableNodeGroup.AutoScalableNodeGroup)
	}
}

func validateHetznerConfig(ctx context.Context) {
	// Ensure that the user has provided Hetzner specific credentials.
	assert.AssertNotNil(ctx, config.ParsedSecretsConfig.Hetzner, "Hetzner credentials not provided")

	// When provisioning a VPN cluster,
	if config.ParsedGeneralConfig.Cluster.Type == constants.ClusterTypeVPN {
		// We must use the Hetzner's hcloud provider.
		assert.Assert(ctx,
			(config.ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHCloud),
			"VPN cluster can only exist in HCloud. So use the hcloud mode of the Hetzner provider")

		// Nil the .cloud.hetzner.hcloudVPNCluster option, if specified.
		config.ParsedGeneralConfig.Cloud.Hetzner.HCloudVPNCluster = nil
	}

	if config.UsingHCloud() {
		validateHCloudConfig(ctx)
	}

	if config.UsingHetznerBareMetal() {
		validateHetznerBareMetalConfig(ctx)
	}
}

func validateHCloudConfig(ctx context.Context) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	// HCloud specific options must be provided.
	assert.AssertNotNil(ctx, hetznerConfig.HCloud, "HCloud specific details not provided")

	// When the control-plane is in HCloud,
	// then HCloud specific control-plane options must be provided.
	if config.ControlPlaneInHCloud() {
		assert.AssertNotNil(ctx, hetznerConfig.ControlPlane.HCloud,
			"HCloud specific control-plane details not provided")
	}

	// Validate auto-scalable node-groups in HCloud.
	for _, hCloudNodeGroup := range hetznerConfig.NodeGroups.HCloud {
		validateAutoScalableNodeGroup(ctx, &hCloudNodeGroup.AutoScalableNodeGroup)
	}
}

func validateHetznerBareMetalConfig(ctx context.Context) {
	// Hetzner Robot username and password must be provided.
	assert.AssertNotNil(ctx, config.ParsedSecretsConfig.Hetzner.Robot,
		"Hetzner robot user and password not provided")

	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	// Hetzner bare-metal specific options must be provided.
	assert.AssertNotNil(ctx, hetznerConfig.BareMetal,
		"Hetzner bare metal specific details not provided")

	// When the cluster is going to be in a Hetzner Network, the user must provide details about
	// the VSwitch : which'll be used to connect the Hetzner Bare Metal servers with that Hetzner
	// Network.
	// TODO : We need this when mode = bare-metal and there is a VPN cluster.
	if hetznerConfig.Mode == constants.HetznerModeHybrid {
		assert.AssertNotNil(ctx, hetznerConfig.BareMetal.VSwitch, "VSwitch details not provided")
	}

	// When the control-plane is in Hetzner bare-metal.
	if config.ControlPlaneInHetznerBareMetal() {
		// Then Hetzner bare-metal specific control-plane options must be provided.
		assert.AssertNotNil(ctx, hetznerConfig.ControlPlane.BareMetal,
			"Hetzner bare metal specific control-plane details not provided")
	}

	// Validate node-groups in Hetzner bare-metal.
	for _, hetznerBaremetalNodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		validateNodeGroup(ctx, &hetznerBaremetalNodeGroup.NodeGroup)
	}
}

func validateBareMetalConfig(ctx context.Context) {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	connector := kubeonessh.NewConnector(ctx)

	// Validate bare-metal hosts.

	for _, host := range bareMetalConfig.ControlPlane.Hosts {
		validateBareMetalHost(ctx, host, connector)
	}

	for _, nodeGroup := range bareMetalConfig.NodeGroups {
		for _, host := range nodeGroup.Hosts {
			validateBareMetalHost(ctx, host, connector)
		}
	}
}

func validateAutoScalableNodeGroup(ctx context.Context,
	autoScalableNodeGroup *config.AutoScalableNodeGroup,
) {
	validateNodeGroup(ctx, &autoScalableNodeGroup.NodeGroup)

	// Validate auto-scaling options.
	assert.Assert(ctx,
		autoScalableNodeGroup.MinSize <= autoScalableNodeGroup.Maxsize,
		"replica count should be <= its max-size",
		slog.String("node-group", autoScalableNodeGroup.Name),
	)
}

func validateNodeGroup(ctx context.Context, nodeGroup *config.NodeGroup) {
	// Validate labels and taints.
	validateLabelsAndTaints(ctx, nodeGroup.Name, nodeGroup.Labels, nodeGroup.Taints)
}

func validateBareMetalHost(ctx context.Context, host *config.BareMetalHost, connector *kubeonessh.Connector) {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	// At least one of public or private IP address must be provided.
	if host.PublicAddress == nil && host.PrivateAddress == nil {
		slog.ErrorContext(ctx, "Neither public, nor private IP address provided for a Bare Metal host")
		os.Exit(1)
	}

	// Validate privateAddress format if provided.
	if host.PrivateAddress != nil {
		parsedPrivateIP := net.ParseIP(*host.PrivateAddress)
		assert.AssertNotNil(ctx, parsedPrivateIP,
			"Invalid private IP address provided for Bare Metal host",
			slog.String("address", *host.PrivateAddress),
		)
	}

	// Build list of addresses to try for SSH.
	// Prefer publicAddress (matching KubeOne behavior), fall back to privateAddress.
	var sshAddresses []string
	if host.PublicAddress != nil {
		sshAddresses = append(sshAddresses, *host.PublicAddress)
	}
	if host.PrivateAddress != nil {
		sshAddresses = append(sshAddresses, *host.PrivateAddress)
	}

	slog.InfoContext(ctx, "Ensuring that the server meets the pre-requisites",
		slog.Any("addresses", sshAddresses),
	)

	/*
		We need to SSH into the host, and ensure the following :

		  (1) We can SSH into the server, as the root user.

		  (2) The server's hostname (stored in /etc/hostname) doesn't contain any uppercase letters.

		  (3) Docker isn't installed there. Otherwise, KubeOne will error out.

		  (4) conntrack and socat packages are installed there. Otherwise, KubeOne will fail during the
		    pre-flight checks.

		  (5) pigz package is installed there. Otherwise ContainerD will fail pulling the OpenEBS
		    dynamic LocalPV Provisioner container image.
	*/

	// Determine the SSH private key to use.
	privateKey := ""
	switch {
	// Use the server sepcific SSH private key, if specified.
	case (host.SSH != nil) && (host.SSH.SSHKeyPairConfig != nil):
		privateKey = host.SSH.PrivateKey

	// Otherwise, use the common SSH private key.
	case bareMetalConfig.SSH.SSHKeyPairConfig != nil:
		privateKey = bareMetalConfig.SSH.PrivateKey

	// Otherwise, either the SSH_AUTH_SOCK environment variable is set,
	// or no private key authentication is required (highly unlikely).
	default:
	}

	// Try SSH connection using each address, preferring publicAddress.
	var connection executor.Interface
	for _, address := range sshAddresses {
		ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("address", address),
		})

		opts := kubeonessh.Opts{
			Context: ctx,

			Hostname:   address,
			Port:       22,
			Username:   "root",
			PrivateKey: []byte(privateKey),

			Timeout: time.Second * 10,
		}
		if len(privateKey) == 0 {
			opts.AgentSocket = os.Getenv(constants.EnvNameSSHAuthSock)
		}

		var err error
		connection, err = kubeonessh.NewConnection(connector, opts)
		if err == nil {
			slog.InfoContext(ctx, "SSH connection established")
			break
		}

		slog.WarnContext(ctx, "SSH connection failed, trying next address",
			logger.Error(err),
		)
	}

	if connection == nil {
		slog.ErrorContext(ctx, "Failed to SSH into server using any address")
		os.Exit(1)
	}
	defer connection.Close()

	slog.DebugContext(ctx, "Opened an SSH connection to the server")

	// Ensure that the server's hostname (stored in /etc/hostname) doesn't contain any uppercase
	// letters.

	command := "cat /etc/hostname"
	slog.DebugContext(ctx, "Executing command", slog.String("command", command))

	hostname, _, _, err := connection.Exec(command)
	assert.AssertErrNil(ctx, err, "Failed determining the server's hostname")

	for _, letter := range hostname {
		assert.Assert(ctx, !unicode.IsUpper(letter),
			"Server's hostname must not contain any uppercase letters",
			slog.String("hostname", hostname),
		)
	}

	// Ensure that the Docker APT repository isn't added using commands which aren't used by KubeOne.
	{
		command = "[ ! -f /etc/apt/sources.list.d/docker.sources ] && [ ! -f /etc/apt/keyrings/docker.asc ]"
		slog.DebugContext(ctx, "Executing command", slog.String("command", command))

		_, _, _, err = connection.Exec(command)
		assert.AssertErrNil(ctx, err, `
Please install the Docker APT repository using commands which KubeOne use :

  		sudo apt-get update
			sudo apt-get install -y apt-transport-https ca-certificates curl lsb-release
			sudo install -m 0755 -d /etc/apt/keyrings
			sudo rm -f /etc/apt/keyrings/docker.gpg
			curl -fsSL https://download.docker.com/linux/$(lsb_release -si | tr '[:upper:]' '[:lower:]')/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
			sudo chmod a+r /etc/apt/keyrings/docker.gpg

			echo "deb [signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$(lsb_release -si | tr '[:upper:]' '[:lower:]') $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list
			sudo apt-get update

REFER : https://github.com/kubermatic/kubeone/blob/225825f44bf38f4c5eca33c76343aed9319413ca/pkg/scripts/render.go#L55.

And remove /etc/apt/sources.list.d/docker.sources and /etc/apt/keyrings/docker.asc.
    `)
	}

	// Ensure that socat, conntrack and pigz are installed.
	packages := []string{"socat", "conntrack", "pigz"}
	for _, p := range packages {
		command := fmt.Sprintf("which %s &> /dev/null", p)
		slog.DebugContext(ctx, "Executing command", slog.String("command", command))

		_, _, _, err := connection.Exec(command)
		assert.AssertErrNil(ctx, err, "All required packages must be installed on the server",
			slog.Any("packages", packages),
		)
	}
}

// A user defined NodeGroup label key should belong to one of these domains.
// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
var validNodeGroupLabelDomains = []string{
	"node.cluster.x-k8s.io/",
	"node-role.kubernetes.io/",
	"node-restriction.kubernetes.io/",
}

// Validates node-group labels and taints.
func validateLabelsAndTaints(ctx context.Context,
	nodeGroupName string,
	labels map[string]string,
	taints []*coreV1.Taint,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("node-group", nodeGroupName),
	})

	// Validate labels.
	//
	// (1) according to Kubernetes specifications.
	err := labelsPkg.Validate(labels)
	assert.AssertErrNil(ctx, err, "MachinePool labels validation failed")
	//
	// (2) according to ClusterAPI specifications.
	for key := range labels {
		// Check if the label belongs to a domain considered valid by ClusterAPI.
		isValidNodeGroupLabelDomain := false
		for _, nodeGroupLabelDomains := range validNodeGroupLabelDomains {
			if strings.HasPrefix(key, nodeGroupLabelDomains) {
				isValidNodeGroupLabelDomain = true
				break
			}
		}
		if !isValidNodeGroupLabelDomain {
			slog.ErrorContext(ctx,
				"NodeGroup label key should belong to one of these domains",
				slog.Any("domains", validNodeGroupLabelDomains),
			)
			os.Exit(1)
		}
	}

	taintsAsKVPairs := map[string]string{}
	for _, taint := range taints {
		taintsAsKVPairs[taint.Key] = fmt.Sprintf("%s:%s", taint.Value, taint.Effect)
	}
	//
	// Validate taints.
	err = labelsPkg.ValidateTaints(taintsAsKVPairs)
	assert.AssertErrNil(ctx, err, "NodeGroup taints validation failed")
}

func validateKubePrometheusVersion(ctx context.Context, kubePrometheusVersion, k8sVersion string) {
	// Ensure that the KubePrometheus version is a valid semantic version.

	hasPrefixV := strings.HasPrefix(kubePrometheusVersion, "v")
	assert.Assert(ctx, hasPrefixV, "KubePrometheus version must start with 'v' (for e.g.: v0.15.0)")

	parsedKubePrometheusVersion, err := version.Parse(kubePrometheusVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing KubePrometheus semantic version")

	// Ensure that the KubePrometheus and K8s versions are officially compatible.

	parsedK8sVersion := version.MustParse(k8sVersion)

	key := fmt.Sprintf("v%d.%d", parsedKubePrometheusVersion.Major(), parsedKubePrometheusVersion.Minor())

	supportedK8sVersions, ok := constants.KubePrometheusKubernetesVersionCompatibilityMatrix[key]
	assert.Assert(ctx, ok, "Unrecognized KubePrometheus version")

	k8sVersionSupported := slices.Contains(supportedK8sVersions,
		fmt.Sprintf("v%d.%d", parsedK8sVersion.Major(), parsedK8sVersion.Minor()))

	assert.Assert(ctx, k8sVersionSupported, `
KubePrometheus and K8s versions aren't officially compatible! You can check the compatibility
matrix here :

    https://github.com/prometheus-operator/kube-prometheus?tab=readme-ov-file#compatibility
  `)
}
