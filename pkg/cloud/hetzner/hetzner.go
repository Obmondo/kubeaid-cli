// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

type serverTypeClient interface {
	GetByName(ctx context.Context, name string) (*hcloud.ServerType, *hcloud.Response, error)
}

type networkClient interface {
	Get(ctx context.Context, idOrName string) (*hcloud.Network, *hcloud.Response, error)
	Create(ctx context.Context, opts hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error)
}

type serverClient interface {
	AttachToNetwork(ctx context.Context, server *hcloud.Server, opts hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error)
	List(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error)
}

type Hetzner struct {
	hcloudClient *hcloud.Client
	robotClient  *resty.Client

	serverTypeClient serverTypeClient
	networkClient    networkClient
	serverClient       serverClient

	sleepFunc func(time.Duration)
}

func NewHetznerCloudProvider() cloud.CloudProvider {
	hetznerClient := &Hetzner{
		sleepFunc: time.Sleep,
	}

	// Construct HCloud client, if we're using HCloud.
	if config.UsingHCloud() {
		hcloudClient := hcloud.NewClient(
			hcloud.WithToken(config.ParsedSecretsConfig.Hetzner.APIToken),
		)

		hetznerClient.hcloudClient = hcloudClient
		hetznerClient.serverTypeClient = &hcloudClient.ServerType
		hetznerClient.networkClient = &hcloudClient.Network
		hetznerClient.serverClient = &hcloudClient.Server
	}

	// Construct Hetzner Robot HTTP client, if we're using Hetzner Bare Metal.
	if config.UsingHetznerBareMetal() {
		robotWebServiceUserCredentials := config.ParsedSecretsConfig.Hetzner.Robot

		hetznerClient.robotClient = resty.New().
			SetBaseURL(constants.HetznerRobotWebServiceAPI).
			SetBasicAuth(robotWebServiceUserCredentials.User, robotWebServiceUserCredentials.Password).
			SetHeader("Content-Type", "application/x-www-form-urlencoded").
			SetHeader("Accept", "application/json")
	}

	return hetznerClient
}

func (*Hetzner) SetupDisasterRecovery(_ context.Context) error {
	return fmt.Errorf("setup disaster recovery is not implemented for Hetzner")
}
