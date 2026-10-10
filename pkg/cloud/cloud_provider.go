// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package cloud

import (
	"context"
)

type (
	CloudProvider interface {
		GetVMSpecs(ctx context.Context, vmType string) (*VMSpec, error)

		SetupDisasterRecovery(ctx context.Context) error

		// Following methods are invoked when upgrading the cluster.

		// While performing a Kubernetes cluster update,
		// this function does updates in the cloud provider specific section of the cluster's
		// values-capi-cluster.yaml file.
		UpdateCapiClusterValuesFile(ctx context.Context, path string, updates any) error
	}

	VMSpec struct {
		CPU    uint32
		Memory uint32 // (in GiB).

		// Only used in case of HCloud, since the root volume size is fixed unlike in case of other
		// hyper-scalars like AWS / Azure.
		RootVolumeSize *uint32
	}
)
