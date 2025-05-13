package cloud

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type (
	CloudProvider interface {
		GetVMSpecs(ctx context.Context, vmType string) *VMSpec

		SetupDisasterRecovery(ctx context.Context)

		UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
			capiClusterValuesFilePath string,
			_updates any,
		)

		UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, _updates any)
	}

	VMSpec struct {
		CPU    uint32
		Memory uint32 // (in MiB).
	}
)
