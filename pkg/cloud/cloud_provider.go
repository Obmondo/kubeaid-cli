package cloud

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CloudProvider interface {
	SetupDisasterRecovery(ctx context.Context)

	GetSealedSecretsBackupBucketName() string

	UpdateMachineTemplate(ctx context.Context, clusterClient client.Client, _updates any)
}
