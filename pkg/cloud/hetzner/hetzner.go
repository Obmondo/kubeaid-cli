// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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

	// sshPool caches SSH connections per bare-metal host for the
	// lifetime of a prereq-infra phase. See pkg/cloud/hetzner/ssh_pool.go
	// for the lifecycle contract — ProvisionPrerequisiteInfrastructure
	// defers sshPool.closeAll() to reclaim cached connections.
	sshPool *sshConnPool

	sleepFunc func(time.Duration)
}

func NewHetznerCloudProvider() cloud.CloudProvider {
	hetznerClient := &Hetzner{
		sleepFunc: time.Sleep,
		sshPool:   newSSHConnPool(),
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
		hetznerClient.robotClient = newRobotRestyClient(
			robotWebServiceUserCredentials.User,
			robotWebServiceUserCredentials.Password,
		)
	}

	return hetznerClient
}

// newRobotRestyClient builds the resty client for the Hetzner Robot web service:
// basic auth plus the form-urlencoded request / JSON response headers the Robot
// API expects.
func newRobotRestyClient(robotUser, robotPassword string) *resty.Client {
	// Reaching the Robot web service is flaky from some networks: its IPv6
	// endpoint frequently times out (Go's dual-stack dialer picks it and stalls),
	// and even IPv4 connects intermittently drop. So pin to IPv4 (robot-ws always
	// has an A record), cap the connect so a stuck dial fails fast, and retry
	// transient failures with backoff. 4xx (incl. 401) are NOT retried — auth will
	// not fix itself, and retrying failed auth can trip Hetzner's lockout.
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		defaultTransport = &http.Transport{}
	}
	transport := defaultTransport.Clone()
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).
			DialContext(ctx, "tcp4", address)
	}

	return resty.New().
		SetBaseURL(constants.HetznerRobotWebServiceAPI).
		SetBasicAuth(robotUser, robotPassword).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json").
		SetTransport(transport).
		SetTimeout(20 * time.Second).
		SetRetryCount(4).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(15 * time.Second).
		AddRetryCondition(func(response *resty.Response, err error) bool {
			return err != nil ||
				response.StatusCode() == http.StatusTooManyRequests ||
				response.StatusCode() >= http.StatusInternalServerError
		})
}

// NewRobotFirewallClient builds a Hetzner client wired only for Hetzner Robot
// firewall operations (EnsureRobotFirewall, with BareMetalIngressRuleset /
// DefaultBareMetalIngressRuleset). It needs nothing but Robot web-service
// credentials — no parsed cluster config, no management or workload cluster — so
// one-off tooling can reconcile a node's public-IP firewall directly against the
// Robot API. The same primitive backs the eventual in-CLI wiring.
func NewRobotFirewallClient(robotUser, robotPassword string) *Hetzner {
	return &Hetzner{
		robotClient: newRobotRestyClient(robotUser, robotPassword),
	}
}

func (*Hetzner) SetupDisasterRecovery(_ context.Context) error {
	return fmt.Errorf("setup disaster recovery is not implemented for Hetzner")
}
