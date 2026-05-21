// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/creasty/defaults"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/version"

	awsCloudProvider "github.com/Obmondo/kubeaid-cli/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/azure"
	"github.com/Obmondo/kubeaid-cli/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
)

// ConfigFilesExist checks whether both general.yaml and secrets.yaml exist at the given path.
func ConfigFilesExist(configsDirectory string) (bool, error) {
	for _, p := range []string{
		config.GetGeneralConfigFilePath(),
		config.GetSecretsConfigFilePath(),
	} {
		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			return false, fmt.Errorf("checking %s: %w", p, err)
		}
	}
	return true, nil
}

func ParseConfigFiles(ctx context.Context, configsDirectory string) {
	var err error

	// Read contents of the general config file into ParsedGeneralConfig.
	{
		config.GeneralConfigFileContents, err = os.ReadFile(config.GetGeneralConfigFilePath())
		assert.AssertErrNil(ctx, err, "Failed reading general config file")

		//nolint:musttag
		err = yaml.Unmarshal([]byte(config.GeneralConfigFileContents), config.ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Failed unmarshalling general config")

		// Set globals.CloudProviderName by detecting the underlying cloud-provider being used.
		detectCloudProviderName()

		// Set defaults.
		err = defaults.Set(config.ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Failed setting defaults for parsed general config")

		forks := &config.ParsedGeneralConfig.Forks

		// When the user has not set the KubeAid Config directory name, default it to the cluster name.
		// The KubeAid config files for the cluster will be generated in that directory.
		if len(forks.KubeaidConfigFork.Directory) == 0 {
			forks.KubeaidConfigFork.Directory = config.ParsedGeneralConfig.Cluster.Name
		}

		// Parse Git repository URLs, and store the result in the config.
		// This will later come handy in a lot of places.
		// Both URLs are required by the config schema (validate:"required").
		forks.KubeaidFork.ParsedURL, err = git.ParseURL(forks.KubeaidFork.URL)
		assert.AssertErrNil(ctx, err, "Failed parsing KubeAid fork URL")
		forks.KubeaidConfigFork.ParsedURL, err = git.ParseURL(forks.KubeaidConfigFork.URL)
		assert.AssertErrNil(ctx, err, "Failed parsing KubeAid Config fork URL")

		// When the user has provided a custom CA certificate path,
		// read and store the custom CA certificate in config.
		hydrateCABundle(ctx)

		// Read SSH keys from provided file paths, validate them and store them in config.
		hydrateSSHKeyPairConfigs()

		// Hydrate with Audit Logging options (if required).
		hydrateWithAuditLoggingOptions()

		// Default cluster.keycloak.realm from DNS when unset (uses
		// publicsuffix). Validation of the typed block happens after
		// defaults so error messages reference the user-visible value.
		//
		// Must run before hydrateKeycloakOIDC and
		// hydrateWithOIDCOptions: both read the resolved realm.
		hydrateKeycloakDefaults()

		// Fill cluster.apiServer.oidc from the cluster.keycloak block
		// so the operator doesn't have to repeat the derivable issuer
		// URL + client ID. Fires for both modes — managed (VPN host)
		// and external (workload cluster referencing a parent VPN's
		// Keycloak, or VPN using an operator-managed Keycloak). No-op
		// when the OIDC block is already set explicitly.
		hydrateKeycloakOIDC()

		// Translate the typed apiServer.oidc block (if any) into the
		// corresponding kube-apiserver AuthenticationConfiguration
		// file + --authentication-config flag + host-path mount.
		hydrateWithOIDCOptions()

		// Default cluster.netbird.{stunDNS,turnDNS,turnUser} when
		// unset. Renders into the netbird Secret consumed by NetBird
		// Mgmt, Dashboard, and Coturn.
		hydrateNetBirdDefaults()

		// Default KubePrometheus version when not explicitly provided.
		err = hydrateKubePrometheusVersion(ctx)
		assert.AssertErrNil(ctx, err, "Failed defaulting KubePrometheus version")
	}

	// Read contents of the secrets config file into ParsedSecretsConfig.
	// This needs to be done before reading the general config.
	{
		secretsConfigFileContents, err := os.ReadFile(config.GetSecretsConfigFilePath())
		assert.AssertErrNil(ctx, err, "Failed reading secrets config file")

		err = yaml.Unmarshal([]byte(secretsConfigFileContents), config.ParsedSecretsConfig)
		assert.AssertErrNil(ctx, err, "Failed unmarshalling secrets config")

		// The AWS credentials and region were not provided via the config file.
		// We'll retrieve them using the files in ~/.aws.
		// And we panic on failure.

		if (globals.CloudProviderName == constants.CloudProviderAWS) &&
			(config.ParsedSecretsConfig.AWS == nil) {
			awsCredentials := mustGetCredentialsFromAWSConfigFile(ctx)

			config.ParsedSecretsConfig.AWS = &config.AWSCredentials{
				AWSAccessKeyID:     awsCredentials.AccessKeyID,
				AWSSecretAccessKey: awsCredentials.SecretAccessKey,
				AWSSessionToken:    awsCredentials.SessionToken,
			}
		}

		// Auto-generate any required-but-blank random secrets
		// (NetBird keys/passwords, Keycloak admin password etc.)
		// and persist them back into secrets.yaml. First run mints
		// fresh values; re-runs are no-ops because the values are
		// already filled in. See FillMissingSecrets for why
		// in-secrets.yaml beats both in-cluster get-or-generate
		// and a separate cache file.
		err = FillMissingSecrets(ctx)
		assert.AssertErrNil(ctx, err, "Failed filling missing secrets in secrets.yaml")
	}

	// Set globals.CloudProvider based on the underlying cloud-provider being used.
	setCloudProvider(ctx)

	/*
		For each node-group (in the general config), the CPU and memory of the corresponding VM type
		need to specified. This is required by Cluster AutoScaler, for 2 things to work :

		  (1) scale from zero

		  (2) when a node in a node-group is cordoned and there is workload-pressure, the node-group
		      gets scaled up.
	*/
	hydrateVMSpecs(ctx)

	// Validate the general and secrets configs.
	err = validateConfigs(ctx)
	assert.AssertErrNil(ctx, err, "Config validation failed")
}

// If the user hasn't specified KubePrometheus version, select the latest
// KubePrometheus version that's officially compatible with the configured K8s version.
func hydrateKubePrometheusVersion(ctx context.Context) error {
	if config.ParsedGeneralConfig.KubePrometheus != nil &&
		len(config.ParsedGeneralConfig.KubePrometheus.Version) > 0 {
		return nil
	}

	parsedK8sVersion, err := version.ParseGeneric(config.ParsedGeneralConfig.Cluster.K8sVersion)
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
	if len(compatibleKubePrometheusVersions) == 0 {
		return fmt.Errorf(
			"no compatible KubePrometheus versions found for Kubernetes version %s",
			k8sMajorMinorVersion,
		)
	}

	sortedCompatibleKubePrometheusVersions := slices.Clone(compatibleKubePrometheusVersions)
	var sortErr error
	slices.SortFunc(sortedCompatibleKubePrometheusVersions, func(a, b string) int {
		if sortErr != nil {
			return 0
		}
		parsedA, err := version.ParseGeneric(a)
		if err != nil {
			sortErr = fmt.Errorf("failed parsing KubePrometheus semantic version %q: %w", a, err)
			return 0
		}

		parsedB, err := version.ParseGeneric(b)
		if err != nil {
			sortErr = fmt.Errorf("failed parsing KubePrometheus semantic version %q: %w", b, err)
			return 0
		}

		cmp, err := parsedA.Compare(parsedB.String())
		if err != nil {
			sortErr = fmt.Errorf("failed comparing KubePrometheus versions: %w", err)
			return 0
		}

		return cmp
	})
	if sortErr != nil {
		return sortErr
	}

	selected := sortedCompatibleKubePrometheusVersions[len(sortedCompatibleKubePrometheusVersions)-1]
	config.ParsedGeneralConfig.KubePrometheus = &config.KubePrometheusConfig{
		Version: selected,
	}

	slog.InfoContext(ctx, "Auto-selected KubePrometheus version",
		slog.String("k8s_version", k8sMajorMinorVersion),
		slog.String("selected_version", selected),
	)
	return nil
}

// Based on the parsed config, detects the underlying cloud-provider name.
// And sets the value of globals.CloudProviderName.
func detectCloudProviderName() {
	switch {
	case config.ParsedGeneralConfig.Cloud.AWS != nil:
		globals.CloudProviderName = constants.CloudProviderAWS

	case config.ParsedGeneralConfig.Cloud.Azure != nil:
		globals.CloudProviderName = constants.CloudProviderAzure

	case config.ParsedGeneralConfig.Cloud.Hetzner != nil:
		globals.CloudProviderName = constants.CloudProviderHetzner

	case config.ParsedGeneralConfig.Cloud.BareMetal != nil:
		globals.CloudProviderName = constants.CloudProviderBareMetal

	case config.ParsedGeneralConfig.Cloud.Local != nil:
		globals.CloudProviderName = constants.CloudProviderLocal

	default:
		slog.Error("No cloud-provider specific details provided")
		os.Exit(1)
	}
}

// Based on the cloud-provider we're using, sets the value of globals.CloudProvider.
func setCloudProvider(ctx context.Context) {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		provider, err := awsCloudProvider.NewAWSCloudProvider()
		assert.AssertErrNil(ctx, err, "Failed creating AWS cloud provider")
		globals.CloudProvider = provider

	case constants.CloudProviderAzure:
		provider, err := azure.NewAzureCloudProvider()
		assert.AssertErrNil(ctx, err, "Failed creating Azure cloud provider")
		globals.CloudProvider = provider

	case constants.CloudProviderHetzner:
		globals.CloudProvider = hetzner.NewHetznerCloudProvider()
	}
}

// Retrieve AWS credentials using the files in ~/.aws.
// Panics on any error.
func mustGetCredentialsFromAWSConfigFile(ctx context.Context) *aws.Credentials {
	slog.InfoContext(ctx, "Trying to pickup AWS credentials from ~/.aws/config")

	awsConfig, err := awsConfig.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing AWS config using files in ~/.aws")

	awsCredentials, err := awsConfig.Credentials.Retrieve(ctx)
	assert.AssertErrNil(ctx, err, "Failed retrieving AWS credentials from constructed AWS config")

	return &awsCredentials
}

// If the user has provided a custom CA certificate path,
// then reads and stores the custom CA certificate in general config.
func hydrateCABundle(ctx context.Context) {
	caBundlePath := config.ParsedGeneralConfig.Git.CABundlePath

	if len(caBundlePath) == 0 {
		return
	}

	caBundle, err := os.ReadFile(caBundlePath)
	assert.AssertErrNil(ctx, err, "Failed reading file", slog.String("path", caBundlePath))

	config.ParsedGeneralConfig.Git.CABundle = caBundle
}

// For each node-group, fills up the cpu and memory (fetched using the corresponding cloud SDK) of
// the corresponding VM type being used.
func hydrateVMSpecs(ctx context.Context) {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		awsConfig := config.ParsedGeneralConfig.Cloud.AWS

		for i, nodeGroup := range awsConfig.NodeGroups {
			instanceSpecs, err := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.InstanceType)
			assert.AssertErrNil(ctx, err, "Failed getting VM specs for instance type",
				slog.String("instance-type", nodeGroup.InstanceType),
			)

			awsConfig.NodeGroups[i].CPU = instanceSpecs.CPU
			awsConfig.NodeGroups[i].Memory = instanceSpecs.Memory
		}

	case constants.CloudProviderAzure:
		azureConfig := config.ParsedGeneralConfig.Cloud.Azure

		for i, nodeGroup := range azureConfig.NodeGroups {
			vmSpecs, err := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.VMSize)
			assert.AssertErrNil(ctx, err, "Failed getting VM specs for VM size",
				slog.String("vm-size", nodeGroup.VMSize),
			)

			azureConfig.NodeGroups[i].CPU = vmSpecs.CPU
			azureConfig.NodeGroups[i].Memory = vmSpecs.Memory
		}

	case constants.CloudProviderHetzner:
		hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

		for i, nodeGroup := range hetznerConfig.NodeGroups.HCloud {
			machineSpecs, err := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.MachineType)
			assert.AssertErrNil(ctx, err, "Failed getting VM specs for machine type",
				slog.String("machine-type", nodeGroup.MachineType),
			)
			assert.AssertNotNil(
				ctx,
				machineSpecs.RootVolumeSize,
				"Implementation error : machine details returned by HetznerCloudProvider.GetVMSpecs() must include RootVolumeSize",
			)

			hetznerConfig.NodeGroups.HCloud[i].CPU = machineSpecs.CPU
			hetznerConfig.NodeGroups.HCloud[i].Memory = machineSpecs.Memory
			hetznerConfig.NodeGroups.HCloud[i].RootVolumeSize = *machineSpecs.RootVolumeSize
		}

	case constants.CloudProviderBareMetal:
	case constants.CloudProviderLocal:
		return

	default:
		panic("unreachable")
	}
}
