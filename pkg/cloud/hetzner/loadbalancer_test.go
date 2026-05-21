// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

//nolint:dupl
type fakeLoadBalancerClient struct {
	getFn                    func(ctx context.Context, idOrName string) (*hcloud.LoadBalancer, *hcloud.Response, error)
	createFn                 func(ctx context.Context, opts hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error)
	updateFn                 func(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error)
	attachToNetworkFn        func(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error)
	enablePublicInterfaceFn  func(ctx context.Context, loadBalancer *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error)
	disablePublicInterfaceFn func(ctx context.Context, loadBalancer *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error)
	changeProtectionFn       func(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error)
	addServiceFn             func(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error)
	addLabelSelectorTargetFn func(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error)
}

func (f *fakeLoadBalancerClient) Get(ctx context.Context, idOrName string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
	return f.getFn(ctx, idOrName)
}

func (f *fakeLoadBalancerClient) Create(ctx context.Context, opts hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
	return f.createFn(ctx, opts)
}

func (f *fakeLoadBalancerClient) Update(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error) {
	return f.updateFn(ctx, loadBalancer, opts)
}

func (f *fakeLoadBalancerClient) AttachToNetwork(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
	return f.attachToNetworkFn(ctx, loadBalancer, opts)
}

func (f *fakeLoadBalancerClient) EnablePublicInterface(ctx context.Context, loadBalancer *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
	return f.enablePublicInterfaceFn(ctx, loadBalancer)
}

func (f *fakeLoadBalancerClient) DisablePublicInterface(ctx context.Context, loadBalancer *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
	return f.disablePublicInterfaceFn(ctx, loadBalancer)
}

func (f *fakeLoadBalancerClient) ChangeProtection(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerChangeProtectionOpts) (*hcloud.Action, *hcloud.Response, error) {
	if f.changeProtectionFn != nil {
		return f.changeProtectionFn(ctx, loadBalancer, opts)
	}
	return nil, &hcloud.Response{Response: &http.Response{StatusCode: http.StatusOK}}, nil
}

func (f *fakeLoadBalancerClient) AddService(ctx context.Context, loadBalancer *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error) {
	if f.addServiceFn != nil {
		return f.addServiceFn(ctx, loadBalancer, opts)
	}
	return nil, &hcloud.Response{Response: &http.Response{StatusCode: http.StatusCreated}}, nil
}

func (f *fakeLoadBalancerClient) AddLabelSelectorTarget(
	ctx context.Context,
	loadBalancer *hcloud.LoadBalancer,
	opts hcloud.LoadBalancerAddLabelSelectorTargetOpts,
) (*hcloud.Action, *hcloud.Response, error) {
	if f.addLabelSelectorTargetFn != nil {
		return f.addLabelSelectorTargetFn(ctx, loadBalancer, opts)
	}
	return nil, &hcloud.Response{Response: &http.Response{StatusCode: http.StatusCreated}}, nil
}

// TestLbHasServiceOnPort pins the pure idempotency-check helper.
func TestLbHasServiceOnPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		lb   *hcloud.LoadBalancer
		port int
		want bool
	}{
		{"nil services slice", &hcloud.LoadBalancer{}, 6443, false},
		{"empty services slice", &hcloud.LoadBalancer{Services: []hcloud.LoadBalancerService{}}, 6443, false},
		{"matching listen port", &hcloud.LoadBalancer{Services: []hcloud.LoadBalancerService{{ListenPort: 6443}}}, 6443, true},
		{"non-matching listen port", &hcloud.LoadBalancer{Services: []hcloud.LoadBalancerService{{ListenPort: 80}}}, 6443, false},
		{"matching among many", &hcloud.LoadBalancer{Services: []hcloud.LoadBalancerService{{ListenPort: 80}, {ListenPort: 443}, {ListenPort: 6443}}}, 6443, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lbHasServiceOnPort(tc.lb, tc.port))
		})
	}
}

// TestLbHasLabelSelectorTarget pins the label-selector idempotency check.
func TestLbHasLabelSelectorTarget(t *testing.T) {
	t.Parallel()
	const sel = "caph-cluster-test=owned,machine_type=control_plane"
	tests := []struct {
		name string
		lb   *hcloud.LoadBalancer
		want bool
	}{
		{"nil targets", &hcloud.LoadBalancer{}, false},
		{
			"matching label-selector target",
			&hcloud.LoadBalancer{Targets: []hcloud.LoadBalancerTarget{{Type: hcloud.LoadBalancerTargetTypeLabelSelector, LabelSelector: &hcloud.LoadBalancerTargetLabelSelector{Selector: sel}}}},
			true,
		},
		{
			"label-selector target with different selector",
			&hcloud.LoadBalancer{
				Targets: []hcloud.LoadBalancerTarget{{Type: hcloud.LoadBalancerTargetTypeLabelSelector, LabelSelector: &hcloud.LoadBalancerTargetLabelSelector{Selector: "other=label"}}},
			},
			false,
		},
		{
			"server-type target ignored even when selector text matches",
			&hcloud.LoadBalancer{Targets: []hcloud.LoadBalancerTarget{{Type: hcloud.LoadBalancerTargetTypeServer, LabelSelector: &hcloud.LoadBalancerTargetLabelSelector{Selector: sel}}}},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lbHasLabelSelectorTarget(tc.lb, sel))
		})
	}
}

// TestEnsureControlPlaneLBServiceAndTarget exercises the orchestrator
// against the fakeLoadBalancerClient. Covers the bootstrap-state cases
// operators actually hit:
//   - fresh LB (no service, no target) → both AddService + AddLabelSelectorTarget
//     are called.
//   - re-run on a kubeaid-cli-wired LB → both calls skipped.
//   - half-wired (service exists, target missing — or vice versa) → only the
//     missing one is added.
//   - AddService API error → surfaces; AddLabelSelectorTarget API error → surfaces.
func TestEnsureControlPlaneLBServiceAndTarget(t *testing.T) {
	t.Parallel()

	const clusterName = "test-cluster"
	wantSelector := controlPlaneLBTargetSelector(clusterName)

	type counters struct{ addService, addTarget int }

	tests := []struct {
		name         string
		initialLB    *hcloud.LoadBalancer
		addServiceFn func(c *counters) func(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error)
		addTargetFn  func(c *counters) func(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error)
		wantErrMsg   string
		wantSvcCalls int
		wantTgtCalls int
	}{
		{
			name:         "fresh LB — both service and target added",
			initialLB:    &hcloud.LoadBalancer{ID: 1},
			wantSvcCalls: 1,
			wantTgtCalls: 1,
		},
		{
			name: "service already wired, target missing — only target added",
			initialLB: &hcloud.LoadBalancer{
				ID:       1,
				Services: []hcloud.LoadBalancerService{{ListenPort: 6443}},
			},
			wantSvcCalls: 0,
			wantTgtCalls: 1,
		},
		{
			name: "target already wired, service missing — only service added",
			initialLB: &hcloud.LoadBalancer{
				ID: 1,
				Targets: []hcloud.LoadBalancerTarget{{
					Type:          hcloud.LoadBalancerTargetTypeLabelSelector,
					LabelSelector: &hcloud.LoadBalancerTargetLabelSelector{Selector: wantSelector},
				}},
			},
			wantSvcCalls: 1,
			wantTgtCalls: 0,
		},
		{
			name: "fully wired — re-run no-op",
			initialLB: &hcloud.LoadBalancer{
				ID:       1,
				Services: []hcloud.LoadBalancerService{{ListenPort: 6443}},
				Targets: []hcloud.LoadBalancerTarget{{
					Type:          hcloud.LoadBalancerTargetTypeLabelSelector,
					LabelSelector: &hcloud.LoadBalancerTargetLabelSelector{Selector: wantSelector},
				}},
			},
			wantSvcCalls: 0,
			wantTgtCalls: 0,
		},
		{
			name:      "AddService API error surfaces",
			initialLB: &hcloud.LoadBalancer{ID: 1},
			addServiceFn: func(c *counters) func(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error) {
				return func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error) {
					c.addService++
					return nil, nil, fmt.Errorf("simulated API failure")
				}
			},
			wantErrMsg:   "adding kube-apiserver service to Hetzner LB",
			wantSvcCalls: 1,
			wantTgtCalls: 0,
		},
		{
			name:      "AddLabelSelectorTarget API error surfaces",
			initialLB: &hcloud.LoadBalancer{ID: 1},
			addTargetFn: func(c *counters) func(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error) {
				return func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error) {
					c.addTarget++
					return nil, nil, fmt.Errorf("simulated target failure")
				}
			},
			wantErrMsg:   "adding control-plane target to Hetzner LB",
			wantSvcCalls: 1,
			wantTgtCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var c counters
			client := &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return tc.initialLB, hcloudResponse(http.StatusOK), nil
				},
				addServiceFn: func() func(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error) {
					if tc.addServiceFn != nil {
						return tc.addServiceFn(&c)
					}
					return func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAddServiceOpts) (*hcloud.Action, *hcloud.Response, error) {
						c.addService++
						return nil, hcloudResponse(http.StatusCreated), nil
					}
				}(),
				addLabelSelectorTargetFn: func() func(ctx context.Context, lb *hcloud.LoadBalancer, opts hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error) {
					if tc.addTargetFn != nil {
						return tc.addTargetFn(&c)
					}
					return func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAddLabelSelectorTargetOpts) (*hcloud.Action, *hcloud.Response, error) {
						c.addTarget++
						return nil, hcloudResponse(http.StatusCreated), nil
					}
				}(),
			}

			h := &Hetzner{loadBalancerClient: client}
			_, err := h.ensureControlPlaneLBServiceAndTarget(context.Background(), tc.initialLB, clusterName)
			if tc.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantSvcCalls, c.addService, "AddService call count")
			assert.Equal(t, tc.wantTgtCalls, c.addTarget, "AddLabelSelectorTarget call count")
		})
	}
}

// noopSleep is used to eliminate real delays in waitForLB.
func noopSleep(time.Duration) {}

func newDisablePublicInterfaceClient(lbID int) *fakeLoadBalancerClient {
	calls := 0
	return &fakeLoadBalancerClient{
		getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
			calls++
			if calls == 1 {
				return &hcloud.LoadBalancer{
					ID:        lbID,
					PublicNet: hcloud.LoadBalancerPublicNet{Enabled: true},
				}, hcloudResponse(http.StatusOK), nil
			}
			return &hcloud.LoadBalancer{
				ID:        lbID,
				PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
			}, hcloudResponse(http.StatusOK), nil
		},
		disablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
			return &hcloud.Action{}, hcloudResponse(http.StatusCreated), nil
		},
	}
}

func TestControlPlaneLBOwnershipLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		want        string
	}{
		{
			name:        "standard cluster name",
			clusterName: "prod",
			want:        "caph-cluster-prod",
		},
		{
			name:        "empty cluster name",
			clusterName: "",
			want:        "caph-cluster-",
		},
		{
			name:        "cluster name with hyphens",
			clusterName: "my-test-cluster",
			want:        "caph-cluster-my-test-cluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := controlPlaneLBOwnershipLabel(tc.clusterName)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLoadBalancerAttachedToNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lb        *hcloud.LoadBalancer
		networkID int
		want      bool
	}{
		{
			name: "attached to matching network",
			lb: &hcloud.LoadBalancer{
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 5}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			networkID: 5,
			want:      true,
		},
		{
			name: "not attached to requested network",
			lb: &hcloud.LoadBalancer{
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 5}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			networkID: 99,
			want:      false,
		},
		{
			name: "empty PrivateNet",
			lb: &hcloud.LoadBalancer{
				PrivateNet: []hcloud.LoadBalancerPrivateNet{},
			},
			networkID: 5,
			want:      false,
		},
		{
			name: "nil Network in PrivateNet entry",
			lb: &hcloud.LoadBalancer{
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: nil, IP: net.ParseIP("10.0.0.1")},
				},
			},
			networkID: 5,
			want:      false,
		},
		{
			name: "nil IP in PrivateNet entry",
			lb: &hcloud.LoadBalancer{
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 5}, IP: nil},
				},
			},
			networkID: 5,
			want:      false,
		},
		{
			name: "multiple entries with second matching",
			lb: &hcloud.LoadBalancer{
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 1}, IP: net.ParseIP("10.0.0.1")},
					{Network: &hcloud.Network{ID: 7}, IP: net.ParseIP("10.0.0.2")},
				},
			},
			networkID: 7,
			want:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := loadBalancerAttachedToNetwork(tc.lb, tc.networkID)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetLB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		getFn      func(ctx context.Context, idOrName string) (*hcloud.LoadBalancer, *hcloud.Response, error)
		wantLB     bool
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "returns existing LB",
			getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
				return &hcloud.LoadBalancer{ID: 1}, hcloudResponse(http.StatusOK), nil
			},
			wantLB: true,
		},
		{
			name: "LB not found returns nil",
			getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
				return nil, hcloudResponse(http.StatusOK), nil
			},
		},
		{
			name: "Get returns error",
			getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
				return nil, nil, fmt.Errorf("network timeout")
			},
			wantErr:    true,
			wantErrMsg: "running Hetzner LB GET operation",
		},
		{
			name: "Get returns unexpected status code",
			getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
				return nil, hcloudResponse(http.StatusInternalServerError), nil
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 500",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				loadBalancerClient: &fakeLoadBalancerClient{getFn: tc.getFn},
			}

			got, err := h.getLB(context.Background(), "test-cluster")
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			if tc.wantLB {
				assert.NotNil(t, got)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestWaitForLB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		polls      []*hcloud.LoadBalancer
		pollErrs   []error
		ready      func(*hcloud.LoadBalancer) bool
		wantErr    bool
		wantErrMsg string
		wantLBID   int
		cancelCtx  bool
	}{
		{
			name: "first poll returns ready LB",
			polls: []*hcloud.LoadBalancer{
				{ID: 10},
			},
			ready:    func(_ *hcloud.LoadBalancer) bool { return true },
			wantLBID: 10,
		},
		{
			name: "second poll returns ready LB",
			polls: []*hcloud.LoadBalancer{
				nil,
				{ID: 20},
			},
			ready:    func(_ *hcloud.LoadBalancer) bool { return true },
			wantLBID: 20,
		},
		{
			name: "non-nil LB but ready returns false then true",
			polls: []*hcloud.LoadBalancer{
				{ID: 30, PrivateNet: nil},
				{ID: 30, PrivateNet: []hcloud.LoadBalancerPrivateNet{{Network: &hcloud.Network{ID: 1}, IP: net.ParseIP("10.0.0.1")}}},
			},
			ready: func(lb *hcloud.LoadBalancer) bool {
				return len(lb.PrivateNet) > 0
			},
			wantLBID: 30,
		},
		{
			name:       "poll returns error",
			pollErrs:   []error{fmt.Errorf("poll failed")},
			ready:      func(_ *hcloud.LoadBalancer) bool { return true },
			wantErr:    true,
			wantErrMsg: "polling LB",
		},
		{
			name:       "context cancelled returns error",
			ready:      func(_ *hcloud.LoadBalancer) bool { return true },
			wantErr:    true,
			wantErrMsg: "waiting for LB",
			cancelCtx:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if tc.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			callIdx := 0
			h := &Hetzner{
				loadBalancerClient: &fakeLoadBalancerClient{
					getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						idx := callIdx
						callIdx++
						if tc.pollErrs != nil && idx < len(tc.pollErrs) && tc.pollErrs[idx] != nil {
							return nil, nil, tc.pollErrs[idx]
						}
						if idx < len(tc.polls) {
							return tc.polls[idx], hcloudResponse(http.StatusOK), nil
						}
						return nil, hcloudResponse(http.StatusOK), nil
					},
				},
				sleepFunc: noopSleep,
			}

			got, err := h.waitForLB(ctx, "test-cluster", tc.ready)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLBID, got.ID)
		})
	}
}

func TestEnsureExistingControlPlaneLB(t *testing.T) {
	t.Parallel()

	network := &hcloud.Network{ID: 42}

	tests := []struct {
		name       string
		lb         *hcloud.LoadBalancer
		client     *fakeLoadBalancerClient
		wantErr    bool
		wantErrMsg string
		wantLBID   int
	}{
		{
			name: "labels already have ownership label",
			lb: &hcloud.LoadBalancer{
				ID: 1,
				Labels: map[string]string{
					controlPlaneLBOwnershipLabel("test-cluster"): "owned",
				},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			client:   &fakeLoadBalancerClient{},
			wantLBID: 1,
		},
		{
			name: "labels need updating and Update succeeds",
			lb: &hcloud.LoadBalancer{
				ID:     2,
				Labels: map[string]string{"existing": "label"},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			client: &fakeLoadBalancerClient{
				updateFn: func(_ context.Context, _ *hcloud.LoadBalancer, opts hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:     2,
						Labels: opts.Labels,
						PrivateNet: []hcloud.LoadBalancerPrivateNet{
							{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
						},
					}, hcloudResponse(http.StatusOK), nil
				},
			},
			wantLBID: 2,
		},
		{
			name: "labels need updating and Update returns error",
			lb: &hcloud.LoadBalancer{
				ID:     3,
				Labels: map[string]string{},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			client: &fakeLoadBalancerClient{
				updateFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("update failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "updating Hetzner LB labels",
		},
		{
			name: "labels need updating and Update returns nil response",
			lb: &hcloud.LoadBalancer{
				ID:     4,
				Labels: map[string]string{},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			client: &fakeLoadBalancerClient{
				updateFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "nil response",
		},
		{
			name: "labels need updating and Update returns unexpected status",
			lb: &hcloud.LoadBalancer{
				ID:     4,
				Labels: map[string]string{},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{
					{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
				},
			},
			client: &fakeLoadBalancerClient{
				updateFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
		{
			name: "LB not attached to network and AttachToNetwork succeeds",
			lb: &hcloud.LoadBalancer{
				ID: 5,
				Labels: map[string]string{
					controlPlaneLBOwnershipLabel("test-cluster"): "owned",
				},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{},
			},
			client: &fakeLoadBalancerClient{
				attachToNetworkFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusCreated), nil
				},
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID: 5,
						PrivateNet: []hcloud.LoadBalancerPrivateNet{
							{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.5")},
						},
					}, hcloudResponse(http.StatusOK), nil
				},
			},
			wantLBID: 5,
		},
		{
			name: "LB not attached to network and AttachToNetwork returns error",
			lb: &hcloud.LoadBalancer{
				ID: 6,
				Labels: map[string]string{
					controlPlaneLBOwnershipLabel("test-cluster"): "owned",
				},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{},
			},
			client: &fakeLoadBalancerClient{
				attachToNetworkFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("attach failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "attaching Hetzner LB to network",
		},
		{
			name: "LB not attached to network and AttachToNetwork returns nil response",
			lb: &hcloud.LoadBalancer{
				ID: 7,
				Labels: map[string]string{
					controlPlaneLBOwnershipLabel("test-cluster"): "owned",
				},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{},
			},
			client: &fakeLoadBalancerClient{
				attachToNetworkFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, nil, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "nil response",
		},
		{
			name: "LB not attached to network and AttachToNetwork returns unexpected status",
			lb: &hcloud.LoadBalancer{
				ID: 8,
				Labels: map[string]string{
					controlPlaneLBOwnershipLabel("test-cluster"): "owned",
				},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{},
			},
			client: &fakeLoadBalancerClient{
				attachToNetworkFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
		{
			name: "AttachToNetwork succeeds but waitForLB returns error",
			lb: &hcloud.LoadBalancer{
				ID: 9,
				Labels: map[string]string{
					controlPlaneLBOwnershipLabel("test-cluster"): "owned",
				},
				PrivateNet: []hcloud.LoadBalancerPrivateNet{},
			},
			client: &fakeLoadBalancerClient{
				attachToNetworkFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerAttachToNetworkOpts) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusCreated), nil
				},
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("poll error")
				},
			},
			wantErr:    true,
			wantErrMsg: "waiting for LB network attachment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				loadBalancerClient: tc.client,
				sleepFunc:          noopSleep,
			}

			got, err := h.ensureExistingControlPlaneLB(context.Background(), tc.lb, "test-cluster", network)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLBID, got.ID)
		})
	}
}

func TestSetControlPlaneLBPublicInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		enabled    bool
		client     *fakeLoadBalancerClient
		wantErr    bool
		wantErrMsg string
		wantNil    bool
		wantLBID   int
	}{
		{
			name:    "getLB returns error",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("get failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "getting LB for public interface toggle",
		},
		{
			name:    "LB is nil returns nil no-op",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
			},
			wantNil: true,
		},
		{
			name:    "LB already in desired state enabled=true",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        1,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: true},
					}, hcloudResponse(http.StatusOK), nil
				},
			},
			wantLBID: 1,
		},
		{
			name:    "LB already in desired state enabled=false",
			enabled: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        2,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
					}, hcloudResponse(http.StatusOK), nil
				},
			},
			wantLBID: 2,
		},
		{
			name:    "enable succeeds",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						if calls == 1 {
							return &hcloud.LoadBalancer{
								ID:        3,
								PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
							}, hcloudResponse(http.StatusOK), nil
						}
						return &hcloud.LoadBalancer{
							ID: 3,
							PublicNet: hcloud.LoadBalancerPublicNet{
								Enabled: true,
								IPv4:    hcloud.LoadBalancerPublicNetIPv4{IP: net.ParseIP("1.2.3.4")},
							},
						}, hcloudResponse(http.StatusOK), nil
					}
				}(),
				enablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantLBID: 3,
		},
		{
			name:     "disable succeeds",
			enabled:  false,
			client:   newDisablePublicInterfaceClient(4),
			wantLBID: 4,
		},
		{
			name:    "enable returns error",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        5,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
					}, hcloudResponse(http.StatusOK), nil
				},
				enablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("enable failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "setting public interface (enabled=true)",
		},
		{
			name:    "disable returns error",
			enabled: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        6,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: true},
					}, hcloudResponse(http.StatusOK), nil
				},
				disablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("disable failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "setting public interface (enabled=false)",
		},
		{
			name:    "enable returns unexpected status",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        7,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
					}, hcloudResponse(http.StatusOK), nil
				},
				enablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
		{
			name:    "enable returns nil response",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        10,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
					}, hcloudResponse(http.StatusOK), nil
				},
				enablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, nil, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "nil response",
		},
		{
			name:    "enable succeeds but waitForLB returns error",
			enabled: true,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						if calls == 1 {
							return &hcloud.LoadBalancer{
								ID:        9,
								PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
							}, hcloudResponse(http.StatusOK), nil
						}
						return nil, nil, fmt.Errorf("poll after enable failed")
					}
				}(),
				enablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "waiting for LB public interface change",
		},
		{
			name:    "disable returns unexpected status",
			enabled: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:        8,
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: true},
					}, hcloudResponse(http.StatusOK), nil
				},
				disablePublicInterfaceFn: func(_ context.Context, _ *hcloud.LoadBalancer) (*hcloud.Action, *hcloud.Response, error) {
					return &hcloud.Action{}, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				loadBalancerClient: tc.client,
				sleepFunc:          noopSleep,
			}

			got, err := h.SetControlPlaneLBPublicInterface(context.Background(), "test-cluster", tc.enabled)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			if tc.wantNil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tc.wantLBID, got.ID)
			}
		})
	}
}

// Mutates config.ParsedGeneralConfig — sequential only.
func TestDisableControlPlaneLBPublicInterface(t *testing.T) {
	setupConfig := func(t *testing.T, cfg *config.GeneralConfig) {
		t.Helper()
		saved := config.ParsedGeneralConfig
		t.Cleanup(func() { config.ParsedGeneralConfig = saved })
		config.ParsedGeneralConfig = cfg
	}

	tests := []struct {
		name       string
		cfg        *config.GeneralConfig
		client     *fakeLoadBalancerClient
		wantErr    bool
		wantErrMsg string
	}{
		{
			// Other modes (bare-metal CP, plain workload without a parent
			// VPN) never had a public HCloud LB to disable — must early
			// return without touching the LB client.
			name: "non-VPN cluster without parent VPN returns early",
			cfg: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						HCloudVPNCluster: nil,
						ControlPlane: config.HetznerControlPlane{
							HCloud: &config.HCloudControlPlane{
								LoadBalancer: config.HCloudControlPlaneLoadBalancer{
									Endpoint: "api.example.com",
								},
							},
						},
					},
				},
				Cluster: config.ClusterConfig{Name: "test-cluster"},
			},
			client: &fakeLoadBalancerClient{},
		},
		{
			// VPN clusters bootstrap their own NetBird mesh — they
			// pre-create the LB themselves (HCloudVPNCluster is nil
			// because there is no parent VPN), so the disable must
			// fire for cluster.type=vpn even without HCloudVPNCluster.
			name: "VPN cluster disables despite nil HCloudVPNCluster",
			cfg: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						HCloudVPNCluster: nil,
						ControlPlane: config.HetznerControlPlane{
							HCloud: &config.HCloudControlPlane{
								LoadBalancer: config.HCloudControlPlaneLoadBalancer{
									Endpoint: "api.vpn.example.com",
								},
							},
						},
					},
				},
				Cluster: config.ClusterConfig{
					Name: "vpn-cluster",
					Type: constants.ClusterTypeVPN,
				},
			},
			client: newDisablePublicInterfaceClient(1),
		},
		{
			name: "hostname is empty returns early",
			cfg: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						HCloudVPNCluster: &config.HCloudVPNClusterConfig{Name: "vpn"},
						ControlPlane: config.HetznerControlPlane{
							HCloud: &config.HCloudControlPlane{
								LoadBalancer: config.HCloudControlPlaneLoadBalancer{
									Endpoint: "",
								},
							},
						},
					},
				},
				Cluster: config.ClusterConfig{Name: "test-cluster"},
			},
			client: &fakeLoadBalancerClient{},
		},
		{
			name: "SetControlPlaneLBPublicInterface returns error",
			cfg: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						HCloudVPNCluster: &config.HCloudVPNClusterConfig{Name: "vpn"},
						ControlPlane: config.HetznerControlPlane{
							HCloud: &config.HCloudControlPlane{
								LoadBalancer: config.HCloudControlPlaneLoadBalancer{
									Endpoint: "api.example.com",
								},
							},
						},
					},
				},
				Cluster: config.ClusterConfig{Name: "test-cluster"},
			},
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("get failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "disabling control-plane LB public interface",
		},
		{
			name: "disable succeeds",
			cfg: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						HCloudVPNCluster: &config.HCloudVPNClusterConfig{Name: "vpn"},
						ControlPlane: config.HetznerControlPlane{
							HCloud: &config.HCloudControlPlane{
								LoadBalancer: config.HCloudControlPlaneLoadBalancer{
									Endpoint: "api.example.com",
								},
							},
						},
					},
				},
				Cluster: config.ClusterConfig{Name: "test-cluster"},
			},
			client: newDisablePublicInterfaceClient(1),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupConfig(t, tc.cfg)

			h := &Hetzner{
				loadBalancerClient: tc.client,
				sleepFunc:          noopSleep,
			}

			err := h.DisableControlPlaneLBPublicInterface(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCreateLB(t *testing.T) {
	t.Parallel()

	network := &hcloud.Network{ID: 42}

	tests := []struct {
		name                  string
		enablePublicInterface bool
		client                *fakeLoadBalancerClient
		wantErr               bool
		wantErrMsg            string
		wantLBID              int
	}{
		{
			name:                  "LB already exists no public interface needed",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID: 1,
						Labels: map[string]string{
							controlPlaneLBOwnershipLabel("test-cluster"): "owned",
						},
						PrivateNet: []hcloud.LoadBalancerPrivateNet{
							{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
						},
						PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
					}, hcloudResponse(http.StatusOK), nil
				},
			},
			wantLBID: 1,
		},
		{
			name:                  "LB already exists public interface enabled",
			enablePublicInterface: true,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						lb := &hcloud.LoadBalancer{
							ID: 2,
							Labels: map[string]string{
								controlPlaneLBOwnershipLabel("test-cluster"): "owned",
							},
							PrivateNet: []hcloud.LoadBalancerPrivateNet{
								{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
							},
							PublicNet: hcloud.LoadBalancerPublicNet{
								Enabled: true,
								IPv4:    hcloud.LoadBalancerPublicNetIPv4{IP: net.ParseIP("1.2.3.4")},
							},
						}
						return lb, hcloudResponse(http.StatusOK), nil
					}
				}(),
			},
			wantLBID: 2,
		},
		{
			name:                  "Get returns error",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("get error")
				},
			},
			wantErr:    true,
			wantErrMsg: "checking for existing LB",
		},
		{
			name:                  "Create returns error",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
					return hcloud.LoadBalancerCreateResult{}, nil, fmt.Errorf("create error")
				},
			},
			wantErr:    true,
			wantErrMsg: "creating Hetzner LB",
		},
		{
			name:                  "Create returns nil response",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
					return hcloud.LoadBalancerCreateResult{}, nil, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "nil response",
		},
		{
			name:                  "Create returns unexpected status code",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, hcloudResponse(http.StatusOK), nil
				},
				createFn: func(_ context.Context, _ hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
					return hcloud.LoadBalancerCreateResult{}, hcloudResponse(http.StatusBadRequest), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 400",
		},
		{
			name:                  "Create succeeds and LB ready immediately without public interface",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						if calls == 1 {
							return nil, hcloudResponse(http.StatusOK), nil
						}
						return &hcloud.LoadBalancer{
							ID: 10,
							PrivateNet: []hcloud.LoadBalancerPrivateNet{
								{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.10")},
							},
							PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
						}, hcloudResponse(http.StatusOK), nil
					}
				}(),
				createFn: func(_ context.Context, _ hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
					return hcloud.LoadBalancerCreateResult{}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantLBID: 10,
		},
		{
			name:                  "Create succeeds and LB ready with public interface",
			enablePublicInterface: true,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						if calls == 1 {
							return nil, hcloudResponse(http.StatusOK), nil
						}
						return &hcloud.LoadBalancer{
							ID: 11,
							PrivateNet: []hcloud.LoadBalancerPrivateNet{
								{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.11")},
							},
							PublicNet: hcloud.LoadBalancerPublicNet{
								Enabled: true,
								IPv4:    hcloud.LoadBalancerPublicNetIPv4{IP: net.ParseIP("5.6.7.8")},
							},
						}, hcloudResponse(http.StatusOK), nil
					}
				}(),
				createFn: func(_ context.Context, _ hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
					return hcloud.LoadBalancerCreateResult{}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantLBID: 11,
		},
		{
			name:                  "Create succeeds but polling Get returns error",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						if calls == 1 {
							return nil, hcloudResponse(http.StatusOK), nil
						}
						return nil, nil, fmt.Errorf("poll error")
					}
				}(),
				createFn: func(_ context.Context, _ hcloud.LoadBalancerCreateOpts) (hcloud.LoadBalancerCreateResult, *hcloud.Response, error) {
					return hcloud.LoadBalancerCreateResult{}, hcloudResponse(http.StatusCreated), nil
				},
			},
			wantErr:    true,
			wantErrMsg: "waiting for LB readiness after creation",
		},
		{
			name:                  "existing LB ensure returns error",
			enablePublicInterface: false,
			client: &fakeLoadBalancerClient{
				getFn: func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return &hcloud.LoadBalancer{
						ID:         1,
						Labels:     map[string]string{},
						PrivateNet: []hcloud.LoadBalancerPrivateNet{},
					}, hcloudResponse(http.StatusOK), nil
				},
				updateFn: func(_ context.Context, _ *hcloud.LoadBalancer, _ hcloud.LoadBalancerUpdateOpts) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					return nil, nil, fmt.Errorf("update failed")
				},
			},
			wantErr:    true,
			wantErrMsg: "ensuring existing control-plane LB",
		},
		{
			name:                  "existing LB enable public interface returns error",
			enablePublicInterface: true,
			client: &fakeLoadBalancerClient{
				getFn: func() func(context.Context, string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
					calls := 0
					return func(_ context.Context, _ string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
						calls++
						if calls == 1 {
							return &hcloud.LoadBalancer{
								ID: 1,
								Labels: map[string]string{
									controlPlaneLBOwnershipLabel("test-cluster"): "owned",
								},
								PrivateNet: []hcloud.LoadBalancerPrivateNet{
									{Network: &hcloud.Network{ID: 42}, IP: net.ParseIP("10.0.0.1")},
								},
								PublicNet: hcloud.LoadBalancerPublicNet{Enabled: false},
							}, hcloudResponse(http.StatusOK), nil
						}
						return nil, nil, fmt.Errorf("get for enable failed")
					}
				}(),
			},
			wantErr:    true,
			wantErrMsg: "enabling public interface on existing LB",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{
				loadBalancerClient: tc.client,
				sleepFunc:          noopSleep,
			}

			got, err := h.CreateLB(context.Background(), "test-cluster", network, "fsn1", tc.enablePublicInterface)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLBID, got.ID)
		})
	}
}
