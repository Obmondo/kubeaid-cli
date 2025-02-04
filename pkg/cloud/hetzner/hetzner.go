package hetzner

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Hetzner struct{}

func NewHetznerCloudProvider() *Hetzner {
	return &Hetzner{}
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

func (*Hetzner) UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, _updates any) {
	panic("unreachable")
}
