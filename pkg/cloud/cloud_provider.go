// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cloud

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type (
	CloudProvider interface {
		GetVMSpecs(ctx context.Context, vmType string) *VMSpec

		SetupDisasterRecovery(ctx context.Context)

		// Following methods are invoked when upgrading the cluster.

		// While performing a Kubernetes cluster update,
		// this function does updates in the cloud provider specific section of the cluster's
		// values-capi-cluster.yaml file.
		UpdateCapiClusterValuesFile(ctx context.Context, path string, updates any)

		// While performing a Kubernetes cluster update,
		// this function recreates the given infrastructure provider specific MachineTemplate resource
		// (like AWSMachineTemplate for AWS), with the required updates, since it can't be updated
		// in-place.
		UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, name string, updates any)
	}

	VMSpec struct {
		CPU    uint32
		Memory uint32 // (in GiB).

		// Only used in case of HCloud, since the root volume size is fixed unlike in case of other
		// hyper-scalars like AWS / Azure.
		RootVolumeSize *uint32
	}
)
