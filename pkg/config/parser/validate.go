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
	if strings.Contains(config.ParsedGeneralConfig.Cluster.Name, ".") {
		return errors.New("cluster name cannot contain any dots")
	}

	if config.ParsedGeneralConfig.Cluster.Type == constants.ClusterTypeVPN &&
		globals.CloudProviderName != constants.CloudProviderHetzner {
		return errors.New("cluster type VPN is supported only for the Hetzner provider as of now")
	}

	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	if err := validator.RegisterValidation(
		"notblank", goNonStandardValidators.NotBlank,
	); err != nil {
		return fmt.Errorf("registering notblank validator: %w", err)
	}

	if err := validator.Struct(config.ParsedGeneralConfig); err != nil {
		return fmt.Errorf("struct validation failed for general config: %w", err)
	}
	if err := validator.Struct(config.ParsedSecretsConfig); err != nil {
		return fmt.Errorf("struct validation failed for secrets config: %w", err)
	}

	if err := validateK8sVersion(ctx, config.ParsedGeneralConfig.Cluster.K8sVersion); err != nil {
		return fmt.Errorf("validating K8s version: %w", err)
	}

	if globals.CloudProviderName != constants.CloudProviderLocal &&
		config.ParsedGeneralConfig.Forks.KubeaidFork.Version == "" {
		return errors.New("KubeAid fork version is required for non-local providers")
	}

	if config.ParsedGeneralConfig.KubePrometheus != nil &&
		config.ParsedGeneralConfig.KubePrometheus.Version != "" {
		if err := validateKubePrometheusVersion(
			config.ParsedGeneralConfig.KubePrometheus.Version,
			config.ParsedGeneralConfig.Cluster.K8sVersion,
		); err != nil {
			return fmt.Errorf("validating KubePrometheus version: %w", err)
		}
	}

	for _, additionalUser := range config.ParsedGeneralConfig.Cluster.AdditionalUsers {
		if additionalUser.Name == "ubuntu" {
			return errors.New("additional user name cannot be 'ubuntu'")
		}
		if _, _, _, _, err := ssh.ParseAuthorizedKey(
			[]byte(additionalUser.SSHPublicKey),
		); err != nil {
			return fmt.Errorf(
				"SSH public key for additional user %q is invalid: %w",
				additionalUser.Name, err,
			)
		}
	}

	// Validate git.knownHosts entries. Each line must parse as an SSH
	// known_hosts entry — garbage here would otherwise land in
	// argocd-ssh-known-hosts-cm and silently break ArgoCD's first clone.
	if err := validateKnownHostsEntries(
		ctx, config.ParsedGeneralConfig.Git.KnownHosts,
	); err != nil {
		return fmt.Errorf("git.knownHosts validation failed: %w", err)
	}

	if config.ParsedGeneralConfig.Obmondo != nil && config.ParsedGeneralConfig.Obmondo.Monitoring {
		if err := validateObmondoMonitoringConfig(); err != nil {
			return fmt.Errorf("validating obmondo monitoring config: %w", err)
		}
	}

	switch globals.CloudProviderName {
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

func validateObmondoMonitoringConfig() error {
	obmondo := config.ParsedGeneralConfig.Obmondo

	if obmondo.CertPath == "" {
		return errors.New(
			"obmondo.monitoring is true but obmondo.certPath is empty" +
				" — an Obmondo-issued mTLS cert is required",
		)
	}
	if obmondo.KeyPath == "" {
		return errors.New(
			"obmondo.monitoring is true but obmondo.keyPath is empty" +
				" — the private key paired with obmondo.certPath is required",
		)
	}
	if _, err := os.Stat(obmondo.CertPath); err != nil {
		return fmt.Errorf("obmondo.certPath %s: %w", obmondo.CertPath, err)
	}
	if _, err := os.Stat(obmondo.KeyPath); err != nil {
		return fmt.Errorf("obmondo.keyPath %s: %w", obmondo.KeyPath, err)
	}

	teleportEnabled := obmondo.TeleportAgent == nil || *obmondo.TeleportAgent
	if teleportEnabled {
		if config.ParsedSecretsConfig.Obmondo == nil {
			return errors.New(
				"obmondo.monitoring is true and obmondo.teleportAgent isn't false" +
					" — secrets.obmondo.teleportAuthToken is required" +
					" (set obmondo.teleportAgent: false to skip teleport-kube-agent)",
			)
		}
		if config.ParsedSecretsConfig.Obmondo.TeleportAuthToken == "" {
			return errors.New(
				"obmondo.monitoring is true and obmondo.teleportAgent isn't false" +
					" — secrets.obmondo.teleportAuthToken is required" +
					" (set obmondo.teleportAgent: false to skip teleport-kube-agent)",
			)
		}
	}
	return nil
}

// validateK8sVersion checks whether the given string represents a valid and
// supported Kubernetes version.
func validateK8sVersion(ctx context.Context, k8sVersion string) error {
	if !strings.HasPrefix(k8sVersion, "v") {
		return errors.New("K8s version must start with 'v' (for e.g.: v1.35.0)")
	}

	semver, err := version.ParseSemantic(k8sVersion)
	if err != nil {
		return fmt.Errorf("parsing K8s semantic version %q: %w", k8sVersion, err)
	}

	// Strip the patch so any patch (e.g. v1.34.6) within a supported minor
	// release is accepted by the inclusive major.minor range check below.
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

	for _, hCloudNodeGroup := range hetznerConfig.NodeGroups.HCloud {
		if err := validateAutoScalableNodeGroup(&hCloudNodeGroup.AutoScalableNodeGroup); err != nil {
			return err
		}
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

func validateKubePrometheusVersion(kubePrometheusVersion, k8sVersion string) error {
	if !strings.HasPrefix(kubePrometheusVersion, "v") {
		return errors.New("KubePrometheus version must start with 'v' (for e.g.: v0.15.0)")
	}

	parsedKubePrometheusVersion, err := version.ParseGeneric(kubePrometheusVersion)
	if err != nil {
		return fmt.Errorf("parsing KubePrometheus semantic version: %w", err)
	}

	parsedK8sVersion, err := version.ParseGeneric(k8sVersion)
	if err != nil {
		return fmt.Errorf("parsing Kubernetes semantic version: %w", err)
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
					"parsing KubePrometheus semantic version %q from compatibility matrix: %w",
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
