// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-resty/resty/v2"
	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

type Hetzner struct {
	hcloudClient *hcloud.Client
	robotClient  *resty.Client
}

func NewHetznerCloudProvider() cloud.CloudProvider {
	// Construct HCloud client.
	hcloudClient := hcloud.NewClient(
		hcloud.WithToken(config.ParsedSecretsConfig.Hetzner.APIToken),
	)

	// Construct Hetzner Robot HTTP client.

	robotWebServiceUserCredentials := config.ParsedSecretsConfig.Hetzner.Robot

	robotClient := resty.New().
		SetBaseURL(constants.HetznerRobotWebServiceAPI).
		SetBasicAuth(robotWebServiceUserCredentials.User, robotWebServiceUserCredentials.Password).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json")

	return &Hetzner{
		hcloudClient,
		robotClient,
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
