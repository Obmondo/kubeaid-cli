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
		Memory uint32 // (in GiB).

		// Only used in case of HCloud, since the root volume size is fixed unlike in case of other
		// hyper-scalars like AWS / Azure.
		RootVolumeSize *uint32
	}
)
