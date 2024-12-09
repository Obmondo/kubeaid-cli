package cloud

import "context"

type CloudProvider interface {
	SetupDisasterRecovery(ctx context.Context)

	GetSealedSecretsBackupBucketName() string
}
