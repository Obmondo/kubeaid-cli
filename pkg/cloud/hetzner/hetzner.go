// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

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
	"github.com/Obmondo/kubeaid-cli/pkg/configquery"
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

type floatingIPClient interface {
	GetByName(ctx context.Context, name string) (*hcloud.FloatingIP, *hcloud.Response, error)
	Create(ctx context.Context, opts hcloud.FloatingIPCreateOpts) (hcloud.FloatingIPCreateResult, *hcloud.Response, error)
	ChangeProtection(ctx context.Context, floatingIP *hcloud.FloatingIP, opts hcloud.FloatingIPChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error)
}

type Hetzner struct {
	hcloudClient *hcloud.Client
	robotClient  *resty.Client
	// robotFailoverClient is the same Robot API with a timeout long
	// enough to hold a Failover IP switch open (90-110s) and retries
	// off. See newRobotFailoverRestyClient.
	robotFailoverClient *resty.Client

	serverTypeClient   serverTypeClient
	networkClient      networkClient
	serverClient       serverClient
	loadBalancerClient loadBalancerClient
	floatingIPClient   floatingIPClient

	// sshPool caches SSH connections per bare-metal host for the
	// lifetime of a prereq-infra phase. See pkg/cloud/hetzner/ssh_pool.go
	// for the lifecycle contract — ProvisionPrerequisiteInfrastructure
	// defers sshPool.closeAll() to reclaim cached connections.
	sshPool *sshConnPool

	sleepFunc func(time.Duration)

	// failoverMaxWait bounds waitForFailoverIP's poll loop. Zero means
	// constants.HRobotFailoverMaxWaitTime; tests shrink it so the
	// give-up path doesn't cost five real minutes.
	failoverMaxWait time.Duration
}

func NewHetznerCloudProvider() cloud.CloudProvider {
	hetznerClient := &Hetzner{
		sleepFunc: time.Sleep,
		sshPool:   newSSHConnPool(),
	}

	// Construct HCloud client, if we're using HCloud.
	if configquery.UsingHCloud() {
		hcloudClient := hcloud.NewClient(
			hcloud.WithToken(config.ParsedSecretsConfig.Hetzner.APIToken),
		)

		hetznerClient.hcloudClient = hcloudClient
		hetznerClient.serverTypeClient = &hcloudClient.ServerType
		hetznerClient.networkClient = &hcloudClient.Network
		hetznerClient.serverClient = &hcloudClient.Server
		hetznerClient.loadBalancerClient = &hcloudClient.LoadBalancer
		hetznerClient.floatingIPClient = &hcloudClient.FloatingIP
	}

	// Construct Hetzner Robot HTTP client, if we're using Hetzner Bare Metal.
	if configquery.UsingHetznerBareMetal() {
		robotWebServiceUserCredentials := config.ParsedSecretsConfig.Hetzner.Robot
		hetznerClient.robotClient = newRobotRestyClient(
			robotWebServiceUserCredentials.User,
			robotWebServiceUserCredentials.Password,
		)
		hetznerClient.robotFailoverClient = newRobotFailoverRestyClient(
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

// newRobotFailoverRestyClient builds the Robot client used for the
// Failover IP switch POST.
//
// Two deliberate departures from the shared client:
//
//   - Timeout long enough to outlast the 90-110s switch. The shared
//     20s guarantees a client-side abort mid-switch.
//   - No retries. The POST is not idempotent in practice — the first
//     request has usually been accepted by the time the client gives
//     up, so a retry just collects a 409 while Robot applies it.
//     pointFailoverIPTo verifies the outcome by polling instead.
func newRobotFailoverRestyClient(robotUser, robotPassword string) *resty.Client {
	return newRobotRestyClient(robotUser, robotPassword).
		SetTimeout(constants.HRobotFailoverSwitchTimeout).
		SetRetryCount(0)
}

func (*Hetzner) SetupDisasterRecovery(_ context.Context) error {
	return fmt.Errorf("setup disaster recovery is not implemented for Hetzner")
}
