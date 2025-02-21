package hetzner

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Hetzner struct{}

func NewHetznerCloudProvider() cloud.CloudProvider {
	return &Hetzner{}
}

func (*Hetzner) GetVMSpecs(ctx context.Context, vmType string) *cloud.VMSpec {
	panic("unimplemented")
}

func (*Hetzner) SetupDisasterRecovery(ctx context.Context) {
	panic("unimplemented")
}

func (*Hetzner) GetSealedSecretsBackupBucketName() string {
	panic("unreachable")
}

func (*Hetzner) GetLatestBackupName(ctx context.Context) string {
	panic("unreachable")
}

func (*Hetzner) UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
	capiClusterValuesFilePath string,
	_updates any,
) {
}

func (*Hetzner) UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, _updates any) {
	panic("unreachable")
}
