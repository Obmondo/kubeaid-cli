// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package hetzner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/storageplanner/storageplan"
)

var _ cloud.CloudProvider = (*Hetzner)(nil)

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

// Hetzner provisions the Hetzner infrastructure of one cluster, the one cfg describes. It is not
// safe for concurrent use; build one per cluster.
type Hetzner struct {
	// cfg is the cluster being provisioned. Provisioning records what it created in
	// cfg.Provisioned and hydrates cfg.General in place.
	cfg *config.Config

	hcloudClient *hcloud.Client
	robotClient  *resty.Client
	// robotNoRetryClient is the same Robot API with retries off and a
	// timeout long enough for Robot's slow, non-idempotent POSTs (Failover
	// IP switch, hardware reset). See newRobotNoRetryRestyClient. Reach
	// it through noRetryRobotClient(), which falls back to robotClient
	// when tests build Hetzner directly.
	robotNoRetryClient *resty.Client

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

	// vSwitchID is the Robot vSwitch CreateVSwitch created or found, for
	// ConnectVSwitchWithHetznerNetwork to attach to the Hetzner Network.
	vSwitchID int

	// approveStoragePlans decides whether the generated storage plans may be applied; see
	// WithStoragePlanApprover. Nil rejects them.
	approveStoragePlans StoragePlanApprover

	// generalConfigPath is where the approved ZFS pool size is written back to; see
	// WithGeneralConfigPath. Empty skips the write.
	generalConfigPath string
}

// StoragePlanApprover decides whether the storage plans generated for the bare-metal servers may
// be applied. Returning an error rejects them.
type StoragePlanApprover func(ctx context.Context, plans storageplan.StoragePlans) error

// Option customises the Hetzner built by NewHetznerCloudProvider.
type Option func(*Hetzner)

// errNoStoragePlanApprover is returned when bare-metal servers are to be provisioned, and no
// StoragePlanApprover was set.
var errNoStoragePlanApprover = errors.New(
	"the storage plans need an explicit approval: build the provisioner with WithStoragePlanApprover",
)

// WithStoragePlanApprover sets who approves the bare-metal storage plans. Without it,
// ProvisionPrerequisiteInfrastructure fails for a bare-metal cluster before touching any server:
// the plans decide which disks get partitioned, so approval must be explicit.
func WithStoragePlanApprover(approver StoragePlanApprover) Option {
	return func(h *Hetzner) { h.approveStoragePlans = approver }
}

// WithGeneralConfigPath makes provisioning write the approved ZFS pool size back into the
// general.yaml at path, so that the file stays self-describing. Without it, nothing is written.
func WithGeneralConfigPath(path string) Option {
	return func(h *Hetzner) { h.generalConfigPath = path }
}

// NewHetznerCloudProvider returns the Hetzner provisioner for cfg, with HCloud and Robot clients
// for the credentials in cfg.Secrets that cfg.General's Hetzner mode needs.
func NewHetznerCloudProvider(cfg *config.Config, opts ...Option) (*Hetzner, error) {
	if cfg == nil || cfg.General == nil || cfg.Secrets == nil {
		return nil, errors.New("a config with general and secrets configs is required")
	}
	if cfg.General.Cloud.Hetzner == nil {
		return nil, errors.New("cloud.hetzner is required")
	}

	hetznerClient := &Hetzner{
		cfg:       cfg,
		sleepFunc: time.Sleep,
		sshPool:   newSSHConnPool(),
	}
	for _, opt := range opts {
		opt(hetznerClient)
	}

	hetznerSecrets := cfg.Secrets.Hetzner

	// Construct HCloud client, if we're using HCloud.
	if cfg.General.UsingHCloud() {
		if hetznerSecrets == nil {
			return nil, errors.New("secrets.hetzner is required when using HCloud")
		}

		hcloudClient := hcloud.NewClient(
			hcloud.WithToken(hetznerSecrets.APIToken),
		)

		hetznerClient.hcloudClient = hcloudClient
		hetznerClient.serverTypeClient = &hcloudClient.ServerType
		hetznerClient.networkClient = &hcloudClient.Network
		hetznerClient.serverClient = &hcloudClient.Server
		hetznerClient.loadBalancerClient = &hcloudClient.LoadBalancer
		hetznerClient.floatingIPClient = &hcloudClient.FloatingIP
	}

	// Construct Hetzner Robot HTTP client, if we're using Hetzner Bare Metal.
	if cfg.General.UsingHetznerBareMetal() {
		if hetznerSecrets == nil || hetznerSecrets.Robot == nil {
			return nil, errors.New("secrets.hetzner.robot is required when using Hetzner Bare Metal")
		}

		robotWebServiceUserCredentials := hetznerSecrets.Robot
		hetznerClient.robotClient = newRobotRestyClient(
			robotWebServiceUserCredentials.User,
			robotWebServiceUserCredentials.Password,
		)
		hetznerClient.robotNoRetryClient = newRobotNoRetryRestyClient(
			robotWebServiceUserCredentials.User,
			robotWebServiceUserCredentials.Password,
		)
	}

	if hetznerClient.hcloudClient == nil && hetznerClient.robotClient == nil {
		return nil, fmt.Errorf("unsupported cloud.hetzner.mode %q", cfg.General.Cloud.Hetzner.Mode)
	}

	return hetznerClient, nil
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

// newRobotNoRetryRestyClient builds the Robot client for POSTs that Robot
// accepts immediately but executes slowly, and that must therefore never be
// retried: the Failover IP switch (POST /failover/{ip}, 90-110s) and the
// hardware reset (POST /reset/{id}).
//
// Two deliberate departures from the shared client:
//
//   - No retries. The shared client retries on `err != nil`, which includes
//     its own 20s timeout, so the retry fires on a request that already
//     SUCCEEDED server-side. For the failover switch that lands a 409 while
//     Robot is still applying it. For the reset it presses the button again
//     on a host that is mid-POST — a server re-reset during BIOS/PXE init
//     can wedge outright and never come back (no SSH, no ping). Running
//     the hosts in parallel makes this far likelier than a manual click,
//     since concurrent load is what pushes Robot past 20s and into 429s
//     (also a retry trigger). Callers verify the outcome by observing the
//     server instead: pointFailoverIPTo polls the Failover IP,
//     bootHBMSIntoRescue waits for the host to drop off the network.
//   - Timeout long enough that a slow-but-successful request isn't aborted
//     client-side. Sized for the longer of the two (the switch).
//
// Reach it through noRetryRobotClient(), not the field.
func newRobotNoRetryRestyClient(robotUser, robotPassword string) *resty.Client {
	return newRobotRestyClient(robotUser, robotPassword).
		SetTimeout(constants.HRobotNoRetryTimeout).
		SetRetryCount(0)
}

// noRetryRobotClient returns the no-retry Robot client, falling back to the
// shared client when the no-retry one wasn't built (tests construct Hetzner
// directly). Shared by the failover switch and the hardware reset.
func (h *Hetzner) noRetryRobotClient() *resty.Client {
	if h.robotNoRetryClient != nil {
		return h.robotNoRetryClient
	}
	return h.robotClient
}

func (*Hetzner) SetupDisasterRecovery(_ context.Context) error {
	return fmt.Errorf("setup disaster recovery is not implemented for Hetzner")
}
