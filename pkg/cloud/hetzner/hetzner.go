// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"

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
	h := &Hetzner{}

	// Construct HCloud client, if we're using HCloud.
	if config.UsingHCloud() {
		h.hcloudClient = hcloud.NewClient(
			hcloud.WithToken(config.ParsedSecretsConfig.Hetzner.APIToken),
		)
	}

	// Construct Hetzner Robot HTTP client, if we're using Hetzner Bare Metal.
	if config.UsingHetznerBareMetal() {
		robotWebServiceUserCredentials := config.ParsedSecretsConfig.Hetzner.Robot

		h.robotClient = resty.New().
			SetBaseURL(constants.HetznerRobotWebServiceAPI).
			SetBasicAuth(robotWebServiceUserCredentials.User, robotWebServiceUserCredentials.Password).
			SetHeader("Content-Type", "application/x-www-form-urlencoded").
			SetHeader("Accept", "application/json")
	}

	return h
}

func (*Hetzner) SetupDisasterRecovery(ctx context.Context) {
	panic("unimplemented")
}

func (*Hetzner) GetLatestBackupName(ctx context.Context) string {
	panic("unreachable")
}
