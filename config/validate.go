package config

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/siderolabs/talos/pkg/machinery/labels"
)

var (
	// A user defined MachinePool label key should belong to one of these domains.
	// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
	ValidMachinePoolLabelDomains = []string{
		"node.cluster.x-k8s.io/",
		"node-role.kubernetes.io/",
		"node-restriction.kubernetes.io/",
	}
)

// Validates the parsed config.
// Panics on failure.
// TODO : Extract the MachinePool labels and taints validation task from 'cloud specifics' section.
func validateConfig(config *Config) {
	// Validate based on struct tags.
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(config); err != nil {
		log.Fatalf("config validation failed : %v", err)
	}

	// Cloud provider specific validations.
	switch {
	case config.Cloud.AWS != nil:

		for _, machinePool := range config.Cloud.AWS.MachinePools {
			// Validate MachinePools labels.
			//
			// (1) according to Kubernetes specifications.
			if err := labels.Validate(machinePool.Labels); err != nil {
				log.Fatalf("MachinePool labels validation failed : %v", err)
			}
			//
			// (2) according to ClusterAPI specifications.
			for key := range machinePool.Labels {
				// Check if the label belongs to a domain considered valid by ClusterAPI.
				isValidMachinePoolLabelDomain := false
				for _, machinePoolLabelDomains := range ValidMachinePoolLabelDomains {
					if strings.HasPrefix(key, machinePoolLabelDomains) {
						isValidMachinePoolLabelDomain = true
						break
					}
				}
				if !isValidMachinePoolLabelDomain {
					slog.Error("MachinePool label key should belong to one of these domains", slog.Any("domains", ValidMachinePoolLabelDomains))
					os.Exit(1)
				}
			}

			taintsAsKVPairs := map[string]string{}
			for _, taint := range machinePool.Taints {
				taintsAsKVPairs[taint.Key] = fmt.Sprintf("%s:%s", taint.Value, taint.Effect)
			}
			//
			// Validate MachinePool taints.
			if err := labels.ValidateTaints(taintsAsKVPairs); err != nil {
				log.Fatalf("MachinePool taint validation failed : %v", err)
			}
		}

	case config.Cloud.Azure != nil:
	case config.Cloud.Hetzner != nil:
		log.Fatal("Support for Azure and Hetzner are coming soon")
	}
}
