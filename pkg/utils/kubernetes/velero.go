package kubernetes

import (
	"context"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	veleroV1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Creates a Velero Backup with the given name.
func CreateBackup(ctx context.Context, name string, clusterClient client.Client) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("backup-name", name),
	})

	backup := veleroV1.Backup{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: constants.NamespaceVelero,
		},

		Spec: veleroV1.BackupSpec{},
	}

	err := clusterClient.Create(ctx, &backup, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed creating Velero Backup")

	slog.InfoContext(ctx, "Created Velero Backup")
}

// Identifies and returns the latest / most recent Velero Backup.
func GetLatestVeleroBackup(ctx context.Context, clusterClient client.Client) *veleroV1.Backup {
	// List all Velero Backups.

	veleroBackupList := veleroV1.BackupList{}

	//nolint:godox
	// TODO : Consider pagination.
	err := clusterClient.List(ctx, &veleroBackupList, &client.ListOptions{
		Namespace: constants.NamespaceVelero,
	})
	assert.AssertErrNil(ctx, err, "Failed listing Velero backups")

	assert.Assert(ctx, len(veleroBackupList.Items) > 0, "No Backups found")

	// Identify the latest / most recent Backup,
	// based on the status.startTimestamp field.

	var (
		latestVeleroBackup          veleroV1.Backup
		latestVeleroBackupStartTime = time.Unix(0, 0)
	)
	for _, veleroBackup := range veleroBackupList.Items {
		veleroBackupStartTime := veleroBackup.Status.StartTimestamp.Time
		if veleroBackupStartTime.After(latestVeleroBackupStartTime) {
			latestVeleroBackup = veleroBackup
			latestVeleroBackupStartTime = veleroBackupStartTime
		}
	}

	slog.InfoContext(ctx,
		"Identified latest / most recent Backup",
		slog.String("backup-name", latestVeleroBackup.Name),
	)

	return &latestVeleroBackup
}

// Creates a Velero Restore object for the given Velero Backup.
func RestoreVeleroBackup(ctx context.Context,
	clusterClient client.Client,
	latestVeleroBackup *veleroV1.Backup,
) {
	veleroRestore := veleroV1.Restore{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      latestVeleroBackup.Name,
			Namespace: latestVeleroBackup.Namespace,
		},

		Spec: veleroV1.RestoreSpec{
			BackupName: latestVeleroBackup.Name,
			RestorePVs: aws.Bool(true),
		},
	}

	err := clusterClient.Create(ctx, &veleroRestore, &client.CreateOptions{})
	assert.AssertErrNil(ctx, err, "Failed listing Velero backups")

	slog.InfoContext(ctx, "Created Velero Restore", slog.String("restore-name", veleroRestore.Name))
}
