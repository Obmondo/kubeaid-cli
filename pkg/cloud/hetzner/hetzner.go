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
	GetByID(ctx context.Context, id int) (*hcloud.Network, *hcloud.Response, error)
	Create(ctx context.Context, opts hcloud.NetworkCreateOpts) (*hcloud.Network, *hcloud.Response, error)
	AddRoute(ctx context.Context, network *hcloud.Network, opts hcloud.NetworkAddRouteOpts) (*hcloud.Action, *hcloud.Response, error)
}

//nolint:dupl // structurally similar to the fakeServerClient test double by nature — an interface and its mock can't be deduplicated.
type serverClient interface {
	AttachToNetwork(ctx context.Context, server *hcloud.Server, opts hcloud.ServerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error)
	List(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error)
	GetByName(ctx context.Context, name string) (*hcloud.Server, *hcloud.Response, error)
	GetByID(ctx context.Context, id int) (*hcloud.Server, *hcloud.Response, error)
	Create(ctx context.Context, opts hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error)
	ChangeProtection(ctx context.Context, server *hcloud.Server, opts hcloud.ServerChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error)
}

//nolint:dupl
type loadBalancerClient interface {
	Get(ctx context.Context, idOrName string) (*hcloud.LoadBalancer, *hcloud.Response, error)
	Create(ctx context.Context, opts hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error)
	Update(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error)
	AttachToNetwork(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error)
	EnablePublicInterface(ctx context.Context, loadBalancer *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error)
	DisablePublicInterface(ctx context.Context, loadBalancer *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error)
	ChangeProtection(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error)
	AddService(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error)
	AddLabelSelectorTarget(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error)
}

type Hetzner struct {
	hcloudClient *hcloud.Client
	robotClient  *resty.Client

	serverTypeClient   serverTypeClient
	networkClient      networkClient
	serverClient       serverClient
	loadBalancerClient loadBalancerClient

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
		hetznerClient.loadBalancerClient = &hcloudClient.LoadBalancer
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
