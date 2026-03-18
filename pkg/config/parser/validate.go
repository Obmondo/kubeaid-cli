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
	"golang.org/x/mod/semver"
	kubeonessh "k8c.io/kubeone/pkg/ssh"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Validates the parsed general and secrets config.
func validateConfigs(ctx context.Context) error {
	if strings.Contains(config.ParsedGeneralConfig.Cluster.Name, ".") {
		return fmt.Errorf("cluster name cannot contain any dots")
	}

	// VPN cluster type is supported only for the Hetzner provider as of now.
	if (config.ParsedGeneralConfig.Cluster.Type == constants.ClusterTypeVPN) &&
		(globals.CloudProviderName != constants.CloudProviderHetzner) {

		return fmt.Errorf("VPN cluster is supported only for the Hetzner provider as of now")
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
	validateKubePrometheusVersion(
		ctx,
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

// Checks whether the given string represents a valid  and supported Kubernetes version or not.
// If not, then panics.
func validateK8sVersion(ctx context.Context, k8sVersion string) {
	assert.Assert(ctx, strings.HasPrefix(k8sVersion, "v"),
		"K8s version must start with 'v' (for example: v1.35.0)",
	)

	parsedK8sVersion, err := version.ParseSemantic(k8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing K8s semantic version")

	const leastSupportedK8sVersion = "v1.30.0"
	parsedLeastSupportedK8sVersion, err := version.ParseSemantic(leastSupportedK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing least supported K8s version")

	latestStableK8sVersion := getLatestStableK8sVersion(ctx)
	parsedLatestStableK8sVersion, err := version.ParseSemantic(latestStableK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing latest stable K8s version")

	// least supported version <= user provided version <= latest stable version.
	//nolint:staticcheck
	if !parsedK8sVersion.AtLeast(parsedLeastSupportedK8sVersion) &&
		!(parsedK8sVersion.LessThan(parsedLatestStableK8sVersion) || parsedK8sVersion.EqualTo(parsedLatestStableK8sVersion)) {

		slog.ErrorContext(ctx, "K8s versions below v1.30.0 aren't supported")
		os.Exit(1)
	}
}

// Fetches and returns the latest stable Kubernetes version from the kubeadm API.
// It will try multiple kubeadm API endpoints, and return the version fetched from the first successful response.
// The error can be produced by a transient network issue, or an issue in the kubeadm API itself.
func getLatestStableK8sVersion(ctx context.Context) string {
	kubeadmAPIURLs := []string{
		"https://dl.k8s.io/release/stable.txt",
	}

	var lastErr error

	for _, kubeadmAPIURL := range kubeadmAPIURLs {
		slog.InfoContext(ctx, "Fetching latest stable K8s version", slog.String("URL", kubeadmAPIURL))

		latestStableK8sVersion, err := fetchLatestStableK8sVersion(kubeadmAPIURL)
		if err == nil {
			return latestStableK8sVersion
		}

		lastErr = fmt.Errorf("failed fetching from %s: %w", kubeadmAPIURL, err)

		slog.WarnContext(ctx,
			"Failed fetching latest stable K8s version, trying next URL",
			logger.Error(lastErr),
		)
	}

	assert.AssertErrNil(ctx, lastErr, "Failed fetching latest stable K8s version")

	return ""
}

func fetchLatestStableK8sVersion(kubeadmAPIURL string) (string, error) {
	response, err := http.Get(kubeadmAPIURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", response.StatusCode)
	}

	latestStableK8sVersion, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	return string(latestStableK8sVersion), nil
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

	// When provisioning a workload cluster, with a VPN cluster hooked up, the user must provide
	// VSwitch details.
	if (config.ParsedGeneralConfig.Cluster.Type == constants.ClusterTypeWorkload) &&
		(config.ParsedGeneralConfig.Cloud.Hetzner.VPNCluster != nil) {

		assert.AssertNotNil(ctx, config.ParsedGeneralConfig.Cloud.Hetzner.VSwitch,
			"VSwitch details not provided",
		)
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

	// If the control-plane is in HCloud,
	// then HCloud specific control-plane options must be provided.
	if config.ControlPlaneInHCloud() {
		assert.AssertNotNil(ctx,
			hetznerConfig.ControlPlane.HCloud,
			"HCloud specific control-plane details not provided",
		)
	}

	// Validate auto-scalable node-groups in HCloud.
	for _, hCloudNodeGroup := range hetznerConfig.NodeGroups.HCloud {
		validateAutoScalableNodeGroup(ctx, &hCloudNodeGroup.AutoScalableNodeGroup)
	}
}

func validateHetznerBareMetalConfig(ctx context.Context) {
	// Hetzner Robot username and password must be provided.
	assert.AssertNotNil(ctx,
		config.ParsedSecretsConfig.Hetzner.Robot,
		"HCloud robot user and password not provided",
	)

	hetznerBareMetalConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	// Hetzner bare-metal specific options must be provided.
	assert.AssertNotNil(ctx,
		hetznerBareMetalConfig.BareMetal,
		"Hetzner bare metal specific details not provided",
	)

	// If the control-plane is in Hetzner bare-metal.
	if config.ControlPlaneInHetznerBareMetal() {
		// Then Hetzner bare-metal specific control-plane options must be provided.
		assert.AssertNotNil(ctx,
			hetznerBareMetalConfig.ControlPlane.BareMetal,
			"Hetzner bare metal specific control-plane details not provided",
		)
	}

	// Validate node-groups in Hetzner bare-metal.
	for _, hetznerBaremetalNodeGroup := range hetznerBareMetalConfig.NodeGroups.BareMetal {
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

func validateBareMetalHost(ctx context.Context,
	host *config.BareMetalHost,
	connector *kubeonessh.Connector,
) {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	k8sVersion := config.ParsedGeneralConfig.Cluster.K8sVersion
	// We would need to update this after each kubeone package upgrade in the codebase. currently kubeone supports k8s version upto v1.34
	if semver.IsValid(k8sVersion) &&
		semver.Compare(semver.MajorMinor(k8sVersion), constants.LatestKubeOneSupportedK8sVersion) > 0 {
		slog.ErrorContext(
			ctx,
			"Latest k8s supported version currently for bare metal is v1.34",
			slog.String("version", k8sVersion),
		)
		os.Exit(1)
	}

	// Either the public or private IP address must be provided.
	// When both are provided, then we'll use the private address to SSH into the server, in the
	// following section.
	var address string
	switch {
	case host.PrivateAddress != nil:
		// The private IP address must not be a hostname.
		parsedPrivateIP := net.ParseIP(*host.PrivateAddress)
		assert.AssertNotNil(ctx, parsedPrivateIP,
			"Invalid private IP address provided for Bare Metal host",
			slog.String("address", *host.PrivateAddress),
		)

		address = *host.PrivateAddress

	case host.PublicAddress != nil:
		address = *host.PublicAddress

	default:
		slog.ErrorContext(ctx, "Neither public, nor private IP address provided for a Bare Metal host")
	}

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("address", address),
	})

	slog.InfoContext(ctx, "Ensuring that the server meets the pre-requisites")

	/*
		We need to SSH into the host, and ensure the following :

		  (1) We can SSH into the server, as the root user.

		  (2) The server's hostname (stored in /etc/hostname) doesn't contain any uppercase letters.
		    We'll also look in the /etc/hosts file, ensuring that localhost and the server's public and
		    private addresses are mapped to that same hostname.

		  (3) Docker isn't installed there. Otherwise, KubeOne will error out.

		  (4) conntrack and socat packages are installed there. Otherwise, KubeOne will fail during the
		    pre-flight checks.

		  (5) pigz package is installed there. Otherwise ContainerD will fail pulling the OpenEBS
		    dynamic LocalPV Provisioner container image.
	*/

	// Determine the SSH private key to use.
	privateKey := ""
	switch {
	// Use the server sepcific SSH private key.
	case (host.SSH != nil) && (host.SSH.PrivateKey != nil):
		privateKey = host.SSH.PrivateKey.PrivateKey

	// Use the common SSH private key.
	case bareMetalConfig.SSH.PrivateKey != nil:
		privateKey = bareMetalConfig.SSH.PrivateKey.PrivateKey

	// Otherwise, either the SSH_AUTH_SOCK environment variable is set,
	// or no private key authentication is required (highly unlikely).
	default:
	}

	// Open an SSH connection to the server.

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

	connection, err := kubeonessh.NewConnection(connector, opts)
	assert.AssertErrNil(ctx, err, "Failed opening SSH connection to server")
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

	// Ensure that Docker isn't installed.
	{
		command = "! which docker &> /dev/null"
		slog.DebugContext(ctx, "Executing command", slog.String("command", command))

		_, _, _, err = connection.Exec(command)
		assert.AssertErrNil(ctx, err, "Docker must not be installed in the server")

		// It might be so that Docker was installed initially. And then, the user uninstalled it.
		// We need to ensure that the APT source and keyring have been removed as well.
		// Otherwise, KubeOne will error out.

		command = "[ ! -f /etc/apt/sources.list.d/docker.sources ] && [ ! -f /etc/apt/keyrings/docker.asc ]"
		slog.DebugContext(ctx, "Executing command", slog.String("command", command))

		_, _, _, err = connection.Exec(command)
		assert.AssertErrNil(ctx, err, "Docker's APT source and keyring must not be added to the server")
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

func validateKubePrometheusVersion(ctx context.Context, kubePrometheusVersion string, k8sVersion string) {
	if config.ParsedGeneralConfig.KubePrometheus.Version == "" {
		return
	}

	if !semver.IsValid(k8sVersion) || !semver.IsValid(kubePrometheusVersion) {
		err := fmt.Errorf(
			"invalid semver format: k8s=%s, kube-prometheus=%s",
			k8sVersion,
			kubePrometheusVersion,
		)
		assert.AssertErrNil(ctx, err, "Version formatting validation failed")
	}

	// To get just major version like v1.34.2 -> v1.34
	k8sVersionTrimmed := semver.MajorMinor(k8sVersion)
	kubePrometheusVersionTrimmed := semver.MajorMinor(kubePrometheusVersion)

	// Source - https://github.com/prometheus-operator/kube-prometheus?tab=readme-ov-file#compatibility
	compatibilityMatrix := map[string][]string{
		"v0.15": {"v1.31", "v1.32", "v1.33"},
		"v0.16": {"v1.31", "v1.32", "v1.33", "v1.34"},
	}

	// Check if kube prometheus version exists in our matrix
	supportedK8s, exists := compatibilityMatrix[kubePrometheusVersionTrimmed]
	if !exists {
		err := fmt.Errorf(
			"kube-prometheus version %s is not in the validation matrix",
			kubePrometheusVersionTrimmed,
		)
		assert.AssertErrNil(ctx, err, "Unknown kube-prometheus version detected")
	}

	// Check if the target K8s version is in the supported list
	isSupported := false
	if slices.Contains(supportedK8s, k8sVersionTrimmed) {
		isSupported = true
		slog.InfoContext(ctx, "Kube Prometheus version is supported", slog.String("version", k8sVersion))
	}

	if !isSupported {
		err := fmt.Errorf(
			"kube-prometheus %s does not support Kubernetes %s. Supported KubePrometheus versions for k8s %v are: %s",
			kubePrometheusVersion,
			k8sVersion,
			supportedK8s,
			kubePrometheusVersionTrimmed,
		)
		assert.AssertErrNil(ctx, err, "Kubernetes version do not supports KubePrometheus version")
	}
}
