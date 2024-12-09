package config

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	validatorV10 "github.com/go-playground/validator/v10"
	goNonStandardValidtors "github.com/go-playground/validator/v10/non-standard/validators"
	labelsPkg "github.com/siderolabs/talos/pkg/machinery/labels"
	coreV1 "k8s.io/api/core/v1"
)

// Validates the parsed config.
func validateConfig(config *Config) {
	ctx := context.Background()

	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	err := validator.RegisterValidation("notblank", goNonStandardValidtors.NotBlank)
	assert.AssertErrNil(ctx, err, "Failed registering notblank validator")

	// Validate based on struct tags.
	err = validator.Struct(config)
	assert.AssertErrNil(ctx, err, "Config validation failed")

	switch {
	case config.Cloud.AWS != nil:
		for _, nodeGroup := range config.Cloud.AWS.NodeGroups {
			// Validate labels and taints.
			validateLabelsAndTaints(ctx, nodeGroup.Name, nodeGroup.Labels, nodeGroup.Taints)
		}

	case config.Cloud.Hetzner != nil:
		break

	case config.Cloud.Azure != nil:
		log.Fatal("Support for Azure is coming soon")

	default:
		log.Fatal("No cloud specific details provided")
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
func validateLabelsAndTaints(ctx context.Context, nodeGroupName string, labels map[string]string, taints []*coreV1.Taint) {
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
			slog.Error("NodeGroup label key should belong to one of these domains", slog.Any("domains", validNodeGroupLabelDomains))
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
