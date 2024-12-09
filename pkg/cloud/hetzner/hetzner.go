package hetzner

import "context"

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
