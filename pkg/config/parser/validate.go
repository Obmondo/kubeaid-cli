// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// validateConfigs validates the parsed general and secrets config.
func validateConfigs(ctx context.Context) error {
	generalConfig := config.ParsedGeneralConfig
	secretsConfig := config.ParsedSecretsConfig
	cloudProviderName := globals.CloudProviderName

	if err := validateConfigFields(
		ctx,
		generalConfig,
		secretsConfig,
		cloudProviderName,
		os.Stat,
	); err != nil {
		return err
	}

	if err := validateK8sVersion(ctx, generalConfig.Cluster.K8sVersion); err != nil {
		return fmt.Errorf("validating K8s version: %w", err)
	}

	if err := validateKeycloakConfig(); err != nil {
		return fmt.Errorf("validating cluster.keycloak: %w", err)
	}
	if generalConfig.KubePrometheus != nil && generalConfig.KubePrometheus.Version != "" {
		if err := validateKubePrometheusVersion(ctx,
			generalConfig.KubePrometheus.Version,
			generalConfig.Cluster.K8sVersion,
		); err != nil {
			return fmt.Errorf("validating KubePrometheus version: %w", err)
		}
	}

	switch cloudProviderName {
	case constants.CloudProviderAWS:
		return validateAWSConfig()
	case constants.CloudProviderAzure:
		return validateAzureConfig()
	case constants.CloudProviderHetzner:
		return validateHetznerConfig()
	case constants.CloudProviderBareMetal:
		return validateBareMetalConfig(ctx)
	case constants.CloudProviderLocal:
		return nil
	}
	return nil
}

type statFunc func(string) (os.FileInfo, error)

func validateConfigFields(
	ctx context.Context,
	generalConfig *config.GeneralConfig,
	secretsConfig *config.SecretsConfig,
	cloudProviderName string,
	stat statFunc,
) error {
	validators := []func() error{
		func() error { return validateClusterName(generalConfig.Cluster.Name) },
		func() error { return validateClusterType(generalConfig.Cluster.Type, cloudProviderName) },
		func() error { return validateConfigStructTags(generalConfig, secretsConfig) },
		func() error {
			return validateKubeAidForkVersion(generalConfig.Forks.KubeaidFork.Version, cloudProviderName)
		},
		func() error { return validateAdditionalUsers(generalConfig.Cluster.AdditionalUsers) },
		func() error { return validateKnownHostsEntries(ctx, generalConfig.Git.KnownHosts) },
		func() error { return validateObmondoMonitoring(generalConfig.Obmondo, secretsConfig.Obmondo, stat) },
	}

	for _, validator := range validators {
		if err := validator(); err != nil {
			return err
		}
	}

	return nil
}

func validateClusterName(clusterName string) error {
	if strings.Contains(clusterName, ".") {
		return errors.New("cluster name cannot contain any dots")
	}
	return nil
}

func validateClusterType(clusterType, cloudProviderName string) error {
	if clusterType == constants.ClusterTypeVPN && cloudProviderName != constants.CloudProviderHetzner {
		return errors.New("cluster type VPN is supported only for the Hetzner provider as of now")
	}
	return nil
}

func validateConfigStructTags(
	generalConfig *config.GeneralConfig,
	secretsConfig *config.SecretsConfig,
) error {
	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	if err := validator.RegisterValidation("notblank", goNonStandardValidators.NotBlank); err != nil {
		return fmt.Errorf("failed registering notblank validator: %w", err)
	}
	if err := validator.Struct(generalConfig); err != nil {
		return fmt.Errorf("struct validation failed for general config: %w", err)
	}
	if err := validator.Struct(secretsConfig); err != nil {
		return fmt.Errorf("struct validation failed for secrets config: %w", err)
	}
	return nil
}

func validateKubeAidForkVersion(kubeAidForkVersion, cloudProviderName string) error {
	if cloudProviderName != constants.CloudProviderLocal && kubeAidForkVersion == "" {
		return errors.New("KubeAid fork version is required for non-local providers")
	}
	return nil
}

func validateAdditionalUsers(additionalUsers []config.UserConfig) error {
	for _, additionalUser := range additionalUsers {
		if additionalUser.Name == "ubuntu" {
			return errors.New("additional user name cannot be ubuntu")
		}
		if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(additionalUser.SSHPublicKey)); err != nil {
			return fmt.Errorf(
				"SSH public key is invalid for additional user %q: %w",
				additionalUser.Name,
				err,
			)
		}
	}
	return nil
}

func validateObmondoMonitoring(
	obmondo *config.ObmondoConfig,
	obmondoCredentials *config.ObmondoCredentials,
	stat statFunc,
) error {
	if obmondo == nil || !obmondo.Monitoring {
		return nil
	}
	if obmondo.CertPath == "" {
		return errors.New(
			"obmondo.monitoring is true but obmondo.certPath is empty, " +
				"an Obmondo-issued mTLS cert is required",
		)
	}
	if obmondo.KeyPath == "" {
		return errors.New(
			"obmondo.monitoring is true but obmondo.keyPath is empty, " +
				"the private key paired with obmondo.certPath is required",
		)
	}
	if _, err := stat(obmondo.CertPath); err != nil {
		return fmt.Errorf("obmondo.certPath does not exist: %w", err)
	}
	if _, err := stat(obmondo.KeyPath); err != nil {
		return fmt.Errorf("obmondo.keyPath does not exist: %w", err)
	}

	teleportEnabled := obmondo.TeleportAgent == nil || *obmondo.TeleportAgent
	if teleportEnabled && (obmondoCredentials == nil || obmondoCredentials.TeleportAuthToken == "") {
		return errors.New(
			"obmondo.monitoring is true and obmondo.teleportAgent isn't false, " +
				"but secrets.obmondo.teleportAuthToken is empty, it's required. " +
				"Set obmondo.teleportAgent: false to skip teleport-kube-agent",
		)
	}

	return nil
}

func validateK8sVersion(ctx context.Context, k8sVersion string) error {
	if !strings.HasPrefix(k8sVersion, "v") {
		return errors.New("K8s version must start with 'v' (for e.g.: v1.35.0)")
	}

	semver, err := version.ParseSemantic(k8sVersion)
	if err != nil {
		return fmt.Errorf("parsing K8s semantic version %q: %w", k8sVersion, err)
	}

	parsedK8sVersion, err := version.ParseMajorMinor(
		fmt.Sprintf("v%d.%d", semver.Major(), semver.Minor()),
	)
	if err != nil {
		return fmt.Errorf("parsing K8s major.minor version %q: %w", k8sVersion, err)
	}

	if err := checkK8sNotReleased(k8sVersion); err != nil {
		return fmt.Errorf("K8s version is not released: %w", err)
	}

	if globals.CloudProviderName == constants.CloudProviderBareMetal {
		parsedMin, err := version.ParseMajorMinor(constants.MinKubeOneSupportedK8sVersion)
		if err != nil {
			return fmt.Errorf("parsing min KubeOne supported K8s version: %w", err)
		}
		parsedMax, err := version.ParseMajorMinor(constants.MaxKubeOneSupportedK8sVersion)
		if err != nil {
			return fmt.Errorf("parsing max KubeOne supported K8s version: %w", err)
		}

		inRange := parsedK8sVersion.AtLeast(parsedMin) &&
			(parsedK8sVersion.LessThan(parsedMax) || parsedK8sVersion.EqualTo(parsedMax))
		if !inRange {
			return fmt.Errorf(
				"K8s version must be in the range (inclusive) : %s - %s for the Bare Metal (KubeOne) provider",
				constants.MinKubeOneSupportedK8sVersion,
				constants.MaxKubeOneSupportedK8sVersion,
			)
		}
	}

	if err := checkK8sLifecycle(ctx, k8sVersion); err != nil {
		return fmt.Errorf("K8s version is not supported: %w", err)
	}
	return nil
}

func validateAWSConfig() error {
	if config.ParsedSecretsConfig.AWS == nil {
		return errors.New("AWS credentials not provided")
	}

	awsConfig := config.ParsedGeneralConfig.Cloud.AWS
	for _, awsAutoScalableNodeGroup := range awsConfig.NodeGroups {
		if err := validateAutoScalableNodeGroup(
			&awsAutoScalableNodeGroup.AutoScalableNodeGroup,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateAzureConfig() error {
	if config.ParsedSecretsConfig.Azure == nil {
		return errors.New("azure credentials not provided")
	}

	azureConfig := config.ParsedGeneralConfig.Cloud.Azure
	for _, azureAutoScalableNodeGroup := range azureConfig.NodeGroups {
		if err := validateAutoScalableNodeGroup(
			&azureAutoScalableNodeGroup.AutoScalableNodeGroup,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateHetznerConfig() error {
	if config.ParsedSecretsConfig.Hetzner == nil {
		return errors.New("hetzner credentials not provided")
	}

	if config.ParsedGeneralConfig.Cluster.Type == constants.ClusterTypeVPN {
		if config.ParsedGeneralConfig.Cloud.Hetzner.Mode != constants.HetznerModeHCloud {
			return errors.New(
				"VPN cluster can only exist in HCloud — set Hetzner mode to hcloud",
			)
		}
		config.ParsedGeneralConfig.Cloud.Hetzner.HCloudVPNCluster = nil
	}

	if config.UsingHCloud() {
		if err := validateHCloudConfig(); err != nil {
			return err
		}
	}
	if config.UsingHetznerBareMetal() {
		if err := validateHetznerBareMetalConfig(); err != nil {
			return err
		}
	}
	return nil
}

func validateHCloudConfig() error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	if hetznerConfig.HCloud == nil {
		return errors.New("HCloud specific details not provided")
	}
	if config.ControlPlaneInHCloud() && hetznerConfig.ControlPlane.HCloud == nil {
		return errors.New("HCloud specific control-plane details not provided")
	}
	if err := validateHCloudControlPlaneLoadBalancerEndpointNotIP(); err != nil {
		return err
	}

	for _, hCloudNodeGroup := range hetznerConfig.NodeGroups.HCloud {
		if err := validateAutoScalableNodeGroup(&hCloudNodeGroup.AutoScalableNodeGroup); err != nil {
			return err
		}
	}
	return nil
}

func validateHCloudControlPlaneLoadBalancerEndpointNotIP() error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	if hetznerConfig == nil || hetznerConfig.ControlPlane.HCloud == nil {
		return nil
	}

	endpoint := hetznerConfig.ControlPlane.HCloud.LoadBalancer.Endpoint
	if endpoint != "" && net.ParseIP(endpoint) != nil {
		return errors.New("control-plane HCloud load-balancer endpoint must be a DNS name, not an IP address")
	}
	return nil
}

func validateHetznerBareMetalConfig() error {
	if config.ParsedSecretsConfig.Hetzner.Robot == nil {
		return errors.New("hetzner robot user and password not provided")
	}

	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	if hetznerConfig.BareMetal == nil {
		return errors.New("hetzner bare metal specific details not provided")
	}

	if hetznerConfig.Mode == constants.HetznerModeHybrid && hetznerConfig.BareMetal.VSwitch == nil {
		return errors.New("VSwitch details not provided")
	}

	if config.ControlPlaneInHetznerBareMetal() && hetznerConfig.ControlPlane.BareMetal == nil {
		return errors.New("hetzner bare metal specific control-plane details not provided")
	}

	for _, hetznerBaremetalNodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		if err := validateNodeGroup(&hetznerBaremetalNodeGroup.NodeGroup); err != nil {
			return err
		}
	}
	return nil
}

func validateBareMetalConfig(ctx context.Context) error {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	connector := kubeonessh.NewConnector(ctx)

	for _, host := range bareMetalConfig.ControlPlane.Hosts {
		if err := validateBareMetalHost(ctx, host, connector); err != nil {
			return err
		}
	}
	for _, nodeGroup := range bareMetalConfig.NodeGroups {
		for _, host := range nodeGroup.Hosts {
			if err := validateBareMetalHost(ctx, host, connector); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateAutoScalableNodeGroup(
	autoScalableNodeGroup *config.AutoScalableNodeGroup,
) error {
	if err := validateNodeGroup(&autoScalableNodeGroup.NodeGroup); err != nil {
		return err
	}
	if autoScalableNodeGroup.MinSize > autoScalableNodeGroup.Maxsize {
		return fmt.Errorf(
			"node-group %q: replica count should be <= its max-size",
			autoScalableNodeGroup.Name,
		)
	}
	return nil
}

func validateNodeGroup(nodeGroup *config.NodeGroup) error {
	return validateLabelsAndTaints(nodeGroup.Name, nodeGroup.Labels, nodeGroup.Taints)
}

func validateBareMetalHost(
	ctx context.Context, host *config.BareMetalHost, connector *kubeonessh.Connector,
) error {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	if host.PublicAddress == nil && host.PrivateAddress == nil {
		return errors.New("neither public, nor private IP address provided for a Bare Metal host")
	}

	if host.PrivateAddress != nil {
		if parsedPrivateIP := net.ParseIP(*host.PrivateAddress); parsedPrivateIP == nil {
			return fmt.Errorf(
				"invalid private IP address %q provided for Bare Metal host",
				*host.PrivateAddress,
			)
		}
	}

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

	privateKey := ""
	switch {
	case host.SSH != nil && host.SSH.SSHKeyPairConfig != nil:
		privateKey = host.SSH.PrivateKey
	case bareMetalConfig.SSH.SSHKeyPairConfig != nil:
		privateKey = bareMetalConfig.SSH.PrivateKey
	default:
	}

	var connection executor.Interface
	for _, address := range sshAddresses {
		ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("address", address),
		})

		opts := kubeonessh.Opts{
			Context:    ctx,
			Hostname:   address,
			Port:       22,
			Username:   "root",
			PrivateKey: []byte(privateKey),
			Timeout:    time.Second * 10,
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
		slog.WarnContext(ctx, "SSH connection failed, trying next address", logger.Error(err))
	}

	if connection == nil {
		return errors.New("failed to SSH into server using any address")
	}
	defer connection.Close()

	slog.DebugContext(ctx, "Opened an SSH connection to the server")

	hostname, _, _, err := connection.Exec("cat /etc/hostname")
	if err != nil {
		return fmt.Errorf("determining server hostname: %w", err)
	}
	for _, letter := range hostname {
		if unicode.IsUpper(letter) {
			return fmt.Errorf(
				"server's hostname %q must not contain any uppercase letters",
				hostname,
			)
		}
	}

	dockerCheckCmd := "[ ! -f /etc/apt/sources.list.d/docker.sources ] && [ ! -f /etc/apt/keyrings/docker.asc ]"
	if _, _, _, err := connection.Exec(dockerCheckCmd); err != nil {
		return fmt.Errorf(`docker APT repository not installed using KubeOne's commands.
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

And remove /etc/apt/sources.list.d/docker.sources and /etc/apt/keyrings/docker.asc: %w`, err)
	}

	packages := []string{"socat", "conntrack", "pigz"}
	for _, p := range packages {
		if _, _, _, err := connection.Exec(fmt.Sprintf("which %s &> /dev/null", p)); err != nil {
			return fmt.Errorf(
				"required package %q missing on server (need: %v): %w",
				p, packages, err,
			)
		}
	}
	return nil
}

// validNodeGroupLabelDomains lists the label-key prefixes ClusterAPI allows
// to be propagated from MachinePool to Node.
// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
var validNodeGroupLabelDomains = []string{
	"node.cluster.x-k8s.io/",
	"node-role.kubernetes.io/",
	"node-restriction.kubernetes.io/",
}

// validateLabelsAndTaints validates node-group labels and taints.
func validateLabelsAndTaints(
	nodeGroupName string,
	labels map[string]string,
	taints []*coreV1.Taint,
) error {
	if err := labelsPkg.Validate(labels); err != nil {
		return fmt.Errorf(
			"MachinePool labels validation failed for node-group %q: %w",
			nodeGroupName, err,
		)
	}

	for key := range labels {
		isValid := false
		for _, prefix := range validNodeGroupLabelDomains {
			if strings.HasPrefix(key, prefix) {
				isValid = true
				break
			}
		}
		if !isValid {
			return fmt.Errorf(
				"NodeGroup label key %q should belong to one of these domains: %v",
				key, validNodeGroupLabelDomains,
			)
		}
	}

	taintsAsKVPairs := map[string]string{}
	for _, taint := range taints {
		taintsAsKVPairs[taint.Key] = fmt.Sprintf("%s:%s", taint.Value, taint.Effect)
	}
	if err := labelsPkg.ValidateTaints(taintsAsKVPairs); err != nil {
		return fmt.Errorf(
			"NodeGroup taints validation failed for node-group %q: %w",
			nodeGroupName, err,
		)
	}
	return nil
}

func validateKubePrometheusVersion(ctx context.Context, kubePrometheusVersion, k8sVersion string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !strings.HasPrefix(kubePrometheusVersion, "v") {
		return errors.New("KubePrometheus version must start with 'v' (for e.g.: v0.15.0)")
	}

	parsedKubePrometheusVersion, err := version.ParseGeneric(kubePrometheusVersion)
	if err != nil {
		return fmt.Errorf("failed parsing KubePrometheus semantic version: %w", err)
	}

	parsedK8sVersion, err := version.ParseGeneric(k8sVersion)
	if err != nil {
		return fmt.Errorf("failed parsing Kubernetes semantic version: %w", err)
	}

	k8sMajorMinorVersion := fmt.Sprintf("v%d.%d", parsedK8sVersion.Major(), parsedK8sVersion.Minor())
	compatibleKubePrometheusVersions, ok := constants.KubernetesKubePrometheusVersionCompatibilityMatrix[k8sMajorMinorVersion]
	if !ok {
		return fmt.Errorf(
			"unsupported Kubernetes version %s for KubePrometheus compatibility matrix",
			k8sMajorMinorVersion,
		)
	}

	var sentinelErr error
	supported := slices.ContainsFunc(
		compatibleKubePrometheusVersions,
		func(compatibleVersion string) bool {
			if sentinelErr != nil {
				return false
			}
			parsedCompatibleVersion, err := version.ParseGeneric(compatibleVersion)
			if err != nil {
				sentinelErr = fmt.Errorf(
					"failed parsing KubePrometheus semantic version %q from compatibility matrix: %w",
					compatibleVersion, err,
				)
				return false
			}
			return parsedCompatibleVersion.Major() == parsedKubePrometheusVersion.Major() &&
				parsedCompatibleVersion.Minor() == parsedKubePrometheusVersion.Minor()
		},
	)
	if sentinelErr != nil {
		return sentinelErr
	}
	if !supported {
		return errors.New(`KubePrometheus and K8s versions aren't officially compatible. See:
    https://github.com/prometheus-operator/kube-prometheus?tab=readme-ov-file#compatibility`)
	}
	return nil
}

// validateKnownHostsEntries returns an error describing the first invalid
// entry, or nil if every entry parses as a valid SSH known_hosts line.
// One entry = one line; multi-line block scalars are rejected so the user
// splits them into separate slice elements (otherwise ParseKnownHosts would
// only check the first line and silently pass garbage after it).
func validateKnownHostsEntries(ctx context.Context, entries []string) error {
	for i, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			return fmt.Errorf("git.knownHosts entry %d is empty", i)
		}
		if strings.Contains(trimmed, "\n") {
			return fmt.Errorf(
				"git.knownHosts entry %d contains multiple lines — "+
					"split each host into its own list element",
				i,
			)
		}
		if _, _, _, _, _, err := ssh.ParseKnownHosts([]byte(trimmed + "\n")); err != nil {
			return fmt.Errorf("git.knownHosts entry %d (%q) is invalid: %w", i, trimmed, err)
		}
		slog.DebugContext(ctx, "git.knownHosts entry validated",
			slog.Int("index", i),
			slog.String("entry", trimmed),
		)
	}
	return nil
}
