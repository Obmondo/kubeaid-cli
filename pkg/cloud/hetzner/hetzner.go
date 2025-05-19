package hetzner

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

type Hetzner struct {
	hcloudClient *hcloud.Client
}

func NewHetznerCloudProvider() cloud.CloudProvider {
	hcloudClient := hcloud.NewClient(
		hcloud.WithToken(config.ParsedSecretsConfig.Hetzner.APIToken),
	)

	return &Hetzner{
		hcloudClient,
	}
}

func (*Hetzner) SetupDisasterRecovery(ctx context.Context) {
	panic("unimplemented")
}

func (*Hetzner) GetLatestBackupName(ctx context.Context) string {
	panic("unreachable")
}

func (*Hetzner) UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx context.Context,
	capiClusterValuesFilePath string,
	_updates any,
) {
}

func (*Hetzner) UpdateMachineTemplate(
	ctx context.Context,
	clusterClient client.Client,
	_updates any,
) {
	panic("unreachable")
}
