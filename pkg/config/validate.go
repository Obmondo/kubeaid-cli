package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	validatorV10 "github.com/go-playground/validator/v10"
	goNonStandardValidtors "github.com/go-playground/validator/v10/non-standard/validators"
	labelsPkg "github.com/siderolabs/talos/pkg/machinery/labels"
	"golang.org/x/crypto/ssh"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Validates the parsed general and secrets config.
func validateConfigs() {
	ctx := context.Background()

	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	err := validator.RegisterValidation("notblank", goNonStandardValidtors.NotBlank)
	assert.AssertErrNil(ctx, err, "Failed registering notblank validator")

	// Validate based on struct tags.
	{
		err = validator.Struct(ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Struct validation failed for general config")

		err = validator.Struct(ParsedSecretsConfig)
		assert.AssertErrNil(ctx, err, "Struct validation failed for secrets config")
	}

	// Validate that cloud provider credentials have been provided.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		assert.AssertNotNil(ctx, ParsedSecretsConfig.AWS, "AWS credentials not provided")

	case constants.CloudProviderAzure:
		assert.AssertNotNil(ctx, ParsedSecretsConfig.Azure, "Azure credentials not provided")

	case constants.CloudProviderHetzner:
		assert.AssertNotNil(ctx, ParsedSecretsConfig.Hetzner, "Hetzner credentials not provided")

		mode := ParsedGeneralConfig.Cloud.Hetzner.Mode

		// Hetzner Robot username and password must be provided, when using Hetzner bare-metal.
		if (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid) {
			assert.AssertNotNil(
				ctx,
				ParsedSecretsConfig.Hetzner.Robot,
				"HCloud robot user and password not provided",
			)
		}
	}

	// Validate K8s version.
	ValidateK8sVersion(ctx, ParsedGeneralConfig.Cluster.K8sVersion)

	// Validate additional users.
	for _, additionalUser := range ParsedGeneralConfig.Cluster.AdditionalUsers {
		// Additional user name cannot be ubuntu.
		assert.Assert(ctx, additionalUser.Name != "ubuntu", "additional user name cannot be ubuntu")

		// Validate the public SSH key.
		_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(additionalUser.SSHPublicKey))
		assert.AssertErrNil(ctx, err,
			"SSH public key is invalid : failed parsing",
			slog.String("additional-user", additionalUser.Name),
		)
	}

	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		// Validate auto-scalable node-groups.
		for _, awsAutoScalableNodeGroup := range ParsedGeneralConfig.Cloud.AWS.NodeGroups {
			validateAutoScalableNodeGroup(ctx, &awsAutoScalableNodeGroup.AutoScalableNodeGroup)
		}

	case constants.CloudProviderAzure:
		// Validate auto-scalable node-groups.
		for _, azureAutoScalableNodeGroup := range ParsedGeneralConfig.Cloud.Azure.NodeGroups {
			validateAutoScalableNodeGroup(ctx, &azureAutoScalableNodeGroup.AutoScalableNodeGroup)
		}

	case constants.CloudProviderHetzner:
		mode := ParsedGeneralConfig.Cloud.Hetzner.Mode

		if mode == constants.HetznerModeBareMetal {
			assert.AssertNotNil(ctx,
				ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.BareMetal,
				"Hetzner bare metal specific details not provided for control-plane",
			)
		} else {
			assert.AssertNotNil(ctx,
				ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud,
				"HCloud specific details not provided for control-plane",
			)
		}

		// When using HCloud.
		if (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid) {
			// Validate auto-scalable node-groups.
			for _, hetznerBaremetalNodeGroup := range ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.HCloud {
				validateAutoScalableNodeGroup(ctx, &hetznerBaremetalNodeGroup.AutoScalableNodeGroup)
			}
		}

		// When using Hetzner bare-metal.
		if (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid) {
			// The rescue HCloud SSH key-pair details must be provided.
			// ClusterAPI Provider Hetzner (CAPH) will create an HCloud SSH key-pair with the given name.
			// So, the user also must ensure that an HCloud SSH key-pair with that name doesn't already
			// exist.
			assert.AssertNotNil(ctx,
				ParsedGeneralConfig.Cloud.Hetzner.RescueHCloudSSHKeyPair,
				"Rescue HCloud SSH key-pair must be provided when using Hetzner Bare Metal",
			)

			// Validate node-groups.
			for _, hetznerBaremetalNodeGroup := range ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.BareMetal {
				validateNodeGroup(ctx, &hetznerBaremetalNodeGroup.NodeGroup)
			}
		}

	case constants.CloudProviderLocal:
		break

	default:
		panic("unreachable")
	}
}

func validateAutoScalableNodeGroup(
	ctx context.Context,
	autoScalableNodeGroup *AutoScalableNodeGroup,
) {
	// Validate auto-scaling options.
	assert.Assert(ctx,
		autoScalableNodeGroup.MinSize <= autoScalableNodeGroup.Maxsize,
		"replica count should be <= its max-size",
		slog.String("node-group", autoScalableNodeGroup.Name),
	)

	validateNodeGroup(ctx, &autoScalableNodeGroup.NodeGroup)
}

func validateNodeGroup(ctx context.Context, nodeGroup *NodeGroup) {
	// Validate labels and taints.
	validateLabelsAndTaints(ctx, nodeGroup.Name, nodeGroup.Labels, nodeGroup.Taints)
}

// Checks whether the given string represents a valid  and supported Kubernetes version or not.
// If not, then panics.
func ValidateK8sVersion(ctx context.Context, k8sVersion string) {
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

// Fetches and returns the latest stable Kubernetes version, from the Kubeadm API endpoint.
func getLatestStableK8sVersion(ctx context.Context) string {
	const kubeadmAPIURL = "https://dl.k8s.io/release/stable.txt"

	slog.InfoContext(ctx, "Fetching latest stable K8s version", slog.String("URL", kubeadmAPIURL))

	response, err := http.Get(kubeadmAPIURL)
	assert.AssertErrNil(ctx, err, "Failed fetching latest stable K8s version")
	if response.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "Failed fetching latest stable Kubernetes version")
		os.Exit(1)
	}
	defer response.Body.Close()

	latestStableK8sVersion, err := io.ReadAll(response.Body)
	assert.AssertErrNil(ctx, err, "Failed reading latest stable K8s version from response body")

	return string(latestStableK8sVersion)
}

// A user defined NodeGroup label key should belong to one of these domains.
// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
var validNodeGroupLabelDomains = []string{
	"node.cluster.x-k8s.io/",
	"node-role.kubernetes.io/",
	"node-restriction.kubernetes.io/",
}

// Validates node-group labels and taints.
func validateLabelsAndTaints(
	ctx context.Context,
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
