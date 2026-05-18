// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"k8s.io/utils/ptr"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// CreateLB creates the Hetzner control-plane LB if it doesn't already
// exist. enablePublicInterface controls whether the LB carries a
// public IPv4 — true during bootstrap (NetBird isn't routing the
// private subnet yet), false in steady state.
func (h *Hetzner) CreateLB(ctx context.Context,
	clusterName string,
	network *hcloud.Network,
	location string,
	enablePublicInterface bool,
) (*hcloud.LoadBalancer, error) {
	existing, err := h.getLB(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("checking for existing LB: %w", err)
	}
	if existing != nil {
		return h.reconcileExistingControlPlaneLB(ctx, existing, clusterName, network, enablePublicInterface)
	}

	result, response, err := h.loadBalancerClient.Create(ctx, hcloud.LoadBalancerCreateOpts{
		Name: clusterName,
		LoadBalancerType: &hcloud.LoadBalancerType{
			Name:        constants.HCloudLBTypeLB11,
			Description: fmt.Sprintf("LB in front of the Kubernetes API server for %s cluster", clusterName),
		},
		Location:        &hcloud.Location{Name: location},
		PublicInterface: ptr.To(enablePublicInterface),
		Network:         network,
		Labels: map[string]string{
			// REFER : https://github.com/syself/cluster-api-provider-hetzner/issues/762#issuecomment-2887786636.
			controlPlaneLBOwnershipLabel(clusterName): "owned",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating Hetzner LB: %w", err)
	}
	if response == nil {
		return nil, fmt.Errorf("creating Hetzner LB: nil response")
	}
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("creating Hetzner LB: unexpected status %d", response.StatusCode)
	}
	slog.InfoContext(ctx, "Created Hetzner LB")

	// Protect the KubeAPI LB from accidental deletion.
	_, response, err = h.loadBalancerClient.ChangeProtection(ctx,
		result.LoadBalancer,
		hcloud.LoadBalancerChangeProtectionOpts{Delete: ptr.To(true)},
	)
	if err != nil {
		return nil, fmt.Errorf("enabling deletion protection on Hetzner LB: %w", err)
	}
	// HCloud's ChangeProtection endpoint creates an Action and returns
	// 201 Created, not 200 OK. Accept both so we don't flag the success
	// case as an error.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("enabling deletion protection on Hetzner LB: unexpected status %d", response.StatusCode)
	}
	slog.InfoContext(ctx, "Enabled deletion protection on Hetzner LB")

	lb, err := h.waitForLB(ctx, clusterName, func(lb *hcloud.LoadBalancer) bool {
		if len(lb.PrivateNet) == 0 {
			return false
		}
		if !enablePublicInterface {
			return true
		}
		return lb.PublicNet.Enabled && lb.PublicNet.IPv4.IP != nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for LB readiness after creation: %w", err)
	}

	lb, err = h.ensureControlPlaneLBServiceAndTarget(ctx, lb, clusterName)
	if err != nil {
		return nil, err
	}
	return lb, nil
}

// reconcileExistingControlPlaneLB brings an already-existing control-
// plane LB up to the desired state: ensures it's attached to the
// network, toggles the public interface, and heals LBs created by an
// older kubeaid-cli that didn't add the service + label-selector
// target (re-runs against an existing-but-unwired LB are the common
// case for operators who hit that bug pre-fix).
func (h *Hetzner) reconcileExistingControlPlaneLB(ctx context.Context,
	existing *hcloud.LoadBalancer,
	clusterName string,
	network *hcloud.Network,
	enablePublicInterface bool,
) (*hcloud.LoadBalancer, error) {
	existing, err := h.ensureExistingControlPlaneLB(ctx, existing, clusterName, network)
	if err != nil {
		return nil, fmt.Errorf("ensuring existing control-plane LB: %w", err)
	}

	if enablePublicInterface {
		existing, err = h.SetControlPlaneLBPublicInterface(ctx, clusterName, true)
		if err != nil {
			return nil, fmt.Errorf("enabling public interface on existing LB: %w", err)
		}
	}

	existing, err = h.ensureControlPlaneLBServiceAndTarget(ctx, existing, clusterName)
	if err != nil {
		return nil, fmt.Errorf("ensuring service/target on existing LB: %w", err)
	}

	slog.InfoContext(ctx, "Hetzner LB already exists")
	return existing, nil
}

// ensureControlPlaneLBServiceAndTarget wires the LB so kube-apiserver
// traffic from the controlPlaneEndpoint actually reaches the control-
// plane nodes:
//
//  1. A TCP service on port 6443 → backend 6443, with a TCP health
//     check on the same port. Without this the LB has no listener for
//     6443 and returns TCP RST ("connection refused") to anyone hitting
//     api.<host>:6443 — including kubeadm's `upload-config` phase
//     which uses the controlPlaneEndpoint, breaking bootstrap before
//     the cluster ever comes up.
//
//  2. A label-selector target keyed on `caph-cluster-<name>=owned`
//     AND `machine_type=control_plane`. CAPH stamps both labels on the
//     HCloud servers it creates for the control plane (server.go:1569
//     in CAPH v1.1.0-alpha.5); the kubeaid-cli-managed NAT gateway
//     only has the first label, so the `machine_type` half ensures we
//     don't accidentally route apiserver traffic to the NAT gateway.
//     UsePrivateIP=true because we're talking to backends across the
//     attached Hetzner Network.
//
// Idempotent: pre-checks `lb.Services` for an existing 6443 listener
// and `lb.Targets` for an existing label-selector target with the
// same selector — skips the API call when either is already present.
// Necessary on re-runs of the bootstrap.
//
// Why we own this instead of CAPH: for VPN clusters, kubeaid-cli
// pre-provisions the LB (so it can sit private-only on the Hetzner
// Network), and the kubeaid chart sets `loadBalancer.enabled=false`
// to stop CAPH creating a second LB. That setting also disables
// CAPH's normal service/target management, so this responsibility
// falls to kubeaid-cli.
func (h *Hetzner) ensureControlPlaneLBServiceAndTarget(
	ctx context.Context, lb *hcloud.LoadBalancer, clusterName string,
) (*hcloud.LoadBalancer, error) {
	if !lbHasServiceOnPort(lb, controlPlaneAPIServerPort) {
		_, response, err := h.loadBalancerClient.AddService(ctx, lb,
			hcloud.LoadBalancerAddServiceOpts{
				Protocol:        hcloud.LoadBalancerServiceProtocolTCP,
				ListenPort:      ptr.To(controlPlaneAPIServerPort),
				DestinationPort: ptr.To(controlPlaneAPIServerPort),
				HealthCheck: &hcloud.LoadBalancerAddServiceOptsHealthCheck{
					Protocol: hcloud.LoadBalancerServiceProtocolTCP,
					Port:     ptr.To(controlPlaneAPIServerPort),
					Interval: ptr.To(15 * time.Second),
					Timeout:  ptr.To(10 * time.Second),
					Retries:  ptr.To(3),
				},
			},
		)
		if err != nil {
			return nil, fmt.Errorf("adding kube-apiserver service to Hetzner LB: %w", err)
		}
		if response == nil {
			return nil, fmt.Errorf("adding kube-apiserver service to Hetzner LB: nil response")
		}
		if response.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("adding kube-apiserver service to Hetzner LB: unexpected status %d", response.StatusCode)
		}
		slog.InfoContext(ctx, "Added kube-apiserver service to Hetzner LB")
	}

	selector := controlPlaneLBTargetSelector(clusterName)
	if !lbHasLabelSelectorTarget(lb, selector) {
		_, response, err := h.loadBalancerClient.AddLabelSelectorTarget(ctx, lb,
			hcloud.LoadBalancerAddLabelSelectorTargetOpts{
				Selector:     selector,
				UsePrivateIP: ptr.To(true),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("adding control-plane target to Hetzner LB: %w", err)
		}
		if response == nil {
			return nil, fmt.Errorf("adding control-plane target to Hetzner LB: nil response")
		}
		if response.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("adding control-plane target to Hetzner LB: unexpected status %d", response.StatusCode)
		}
		slog.InfoContext(ctx, "Added control-plane label-selector target to Hetzner LB",
			slog.String("selector", selector))
	}

	// Re-fetch so the returned LB reflects the new service + target.
	refreshed, err := h.getLB(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("re-fetching LB after service/target wiring: %w", err)
	}
	if refreshed != nil {
		return refreshed, nil
	}
	return lb, nil
}

// controlPlaneAPIServerPort is the standard kube-apiserver port.
// Hardcoded — every kubeaid-cli-bootstrapped cluster uses 6443.
const controlPlaneAPIServerPort = 6443

// controlPlaneLBTargetSelector returns the Hetzner LB target selector
// that picks only CAPH-created control-plane servers. See the
// ensureControlPlaneLBServiceAndTarget comment for the full reasoning.
func controlPlaneLBTargetSelector(clusterName string) string {
	return fmt.Sprintf("%s=owned,machine_type=control_plane",
		controlPlaneLBOwnershipLabel(clusterName))
}

// lbHasServiceOnPort reports whether lb already has a service listening
// on listenPort. Idempotency check for ensureControlPlaneLBServiceAndTarget.
func lbHasServiceOnPort(lb *hcloud.LoadBalancer, listenPort int) bool {
	for _, s := range lb.Services {
		if s.ListenPort == listenPort {
			return true
		}
	}
	return false
}

// lbHasLabelSelectorTarget reports whether lb already has a
// label-selector target with the given selector. Idempotency check.
func lbHasLabelSelectorTarget(lb *hcloud.LoadBalancer, selector string) bool {
	for _, t := range lb.Targets {
		if t.Type == hcloud.LoadBalancerTargetTypeLabelSelector &&
			t.LabelSelector != nil &&
			t.LabelSelector.Selector == selector {
			return true
		}
	}
	return false
}

// SetControlPlaneLBPublicInterface ensures the LB's public interface
// matches enabled. Idempotent — no-op when already in the desired
// state. Used both during bootstrap (enable) and post-bootstrap
// finalize (disable) once NetBird routes the private endpoint.
func (h *Hetzner) SetControlPlaneLBPublicInterface(
	ctx context.Context, clusterName string, enabled bool,
) (*hcloud.LoadBalancer, error) {
	lb, err := h.getLB(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("getting LB for public interface toggle: %w", err)
	}
	if lb == nil || lb.PublicNet.Enabled == enabled {
		return lb, nil
	}

	var (
		response *hcloud.Response
		ready    func(*hcloud.LoadBalancer) bool
		logMsg   string
	)
	if enabled {
		_, response, err = h.loadBalancerClient.EnablePublicInterface(ctx, lb)
		ready = func(lb *hcloud.LoadBalancer) bool {
			return lb.PublicNet.Enabled && lb.PublicNet.IPv4.IP != nil
		}
		logMsg = "Enabled public interface on Hetzner LB"
	} else {
		_, response, err = h.loadBalancerClient.DisablePublicInterface(ctx, lb)
		ready = func(lb *hcloud.LoadBalancer) bool { return !lb.PublicNet.Enabled }
		logMsg = "Disabled public interface on Hetzner LB"
	}
	if err != nil {
		return nil, fmt.Errorf("setting public interface (enabled=%t) on Hetzner LB: %w", enabled, err)
	}
	if response == nil {
		return nil, fmt.Errorf("setting public interface (enabled=%t) on Hetzner LB: nil response", enabled)
	}
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("setting public interface (enabled=%t) on Hetzner LB: unexpected status %d", enabled, response.StatusCode)
	}

	lb, err = h.waitForLB(ctx, clusterName, ready)
	if err != nil {
		return nil, fmt.Errorf("waiting for LB public interface change: %w", err)
	}
	slog.InfoContext(ctx, logMsg)
	return lb, nil
}

// DisableControlPlaneLBPublicInterface is the post-bootstrap finalize
// call: clusters whose control-plane LB ran with a public interface
// during bootstrap (so kubeadm join, ArgoCD setup, etc. could reach
// the apiserver before NetBird was up) flip it off here, leaving the
// LB reachable only through its private IP via the NetBird mesh.
//
// Applies to the two modes that pre-create the LB during bootstrap:
//
//   - VPN clusters: this cluster IS the VPN, bootstraps behind a
//     public LB and transitions to private-via-NetBird once netbird
//     itself is Healthy. Until v1.x of this code the guard skipped
//     these because HCloudVPNCluster is nil for the VPN itself,
//     which left the control-plane public on a freshly bootstrapped
//     VPN cluster — fixed by including cluster.type=vpn in the gate.
//
//   - workload clusters connecting to an existing VPN: same public-
//     during-join / private-after-NetBird lifecycle, signaled by a
//     non-nil HCloudVPNCluster pointing at the parent VPN.
//
// Other modes (bare-metal CP, plain workload without a parent VPN)
// never had a public HCloud LB to disable — early return.
//
// Also gated on hostname being configured: without an FQDN the LB's
// public IP is the operator's only entry point to kube-apiserver,
// and disabling it would lock them out of their own cluster.
func (h *Hetzner) DisableControlPlaneLBPublicInterface(ctx context.Context) error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	cluster := config.ParsedGeneralConfig.Cluster

	preCreatedLB := cluster.Type == constants.ClusterTypeVPN ||
		hetznerConfig.HCloudVPNCluster != nil
	if !preCreatedLB {
		return nil
	}
	if hetznerConfig.ControlPlane.HCloud.LoadBalancer.Endpoint == "" {
		return nil
	}

	_, err := h.SetControlPlaneLBPublicInterface(ctx, cluster.Name, false)
	if err != nil {
		return fmt.Errorf("disabling control-plane LB public interface: %w", err)
	}
	return nil
}

func (h *Hetzner) ensureExistingControlPlaneLB(ctx context.Context,
	loadBalancer *hcloud.LoadBalancer,
	clusterName string,
	network *hcloud.Network,
) (*hcloud.LoadBalancer, error) {
	ownershipLabel := controlPlaneLBOwnershipLabel(clusterName)
	if loadBalancer.Labels[ownershipLabel] != "owned" {
		labels := map[string]string{}
		maps.Copy(labels, loadBalancer.Labels)
		labels[ownershipLabel] = "owned"

		updated, response, err := h.loadBalancerClient.Update(ctx, loadBalancer,
			hcloud.LoadBalancerUpdateOpts{Labels: labels},
		)
		if err != nil {
			return nil, fmt.Errorf("updating Hetzner LB labels: %w", err)
		}
		if response == nil {
			return nil, fmt.Errorf("updating Hetzner LB labels: nil response")
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("updating Hetzner LB labels: unexpected status %d", response.StatusCode)
		}
		loadBalancer = updated
	}

	if !loadBalancerAttachedToNetwork(loadBalancer, network.ID) {
		_, response, err := h.loadBalancerClient.AttachToNetwork(ctx, loadBalancer,
			hcloud.LoadBalancerAttachToNetworkOpts{Network: network},
		)
		if err != nil {
			return nil, fmt.Errorf("attaching Hetzner LB to network: %w", err)
		}
		if response == nil {
			return nil, fmt.Errorf("attaching Hetzner LB to network: nil response")
		}
		if response.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("attaching Hetzner LB to network: unexpected status %d", response.StatusCode)
		}
		lb, err := h.waitForLB(ctx, clusterName, func(lb *hcloud.LoadBalancer) bool {
			return loadBalancerAttachedToNetwork(lb, network.ID)
		})
		if err != nil {
			return nil, fmt.Errorf("waiting for LB network attachment: %w", err)
		}
		return lb, nil
	}

	return loadBalancer, nil
}

func (h *Hetzner) getLB(ctx context.Context, clusterName string) (*hcloud.LoadBalancer, error) {
	lb, response, err := h.loadBalancerClient.Get(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("running Hetzner LB GET operation: %w", err)
	}
	if response != nil && response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("running Hetzner LB GET operation: unexpected status %d", response.StatusCode)
	}
	return lb, nil
}

func (h *Hetzner) waitForLB(ctx context.Context,
	clusterName string,
	ready func(*hcloud.LoadBalancer) bool,
) (*hcloud.LoadBalancer, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for LB %q: %w", clusterName, ctx.Err())
		default:
		}

		h.sleepFunc(10 * time.Second)

		lb, err := h.getLB(ctx, clusterName)
		if err != nil {
			return nil, fmt.Errorf("polling LB %q: %w", clusterName, err)
		}
		if lb != nil && ready(lb) {
			return lb, nil
		}
	}
}

func controlPlaneLBOwnershipLabel(clusterName string) string {
	return fmt.Sprintf("caph-cluster-%s", clusterName)
}

func loadBalancerAttachedToNetwork(lb *hcloud.LoadBalancer, networkID int) bool {
	for _, privateNet := range lb.PrivateNet {
		if privateNet.Network != nil && privateNet.Network.ID == networkID && privateNet.IP != nil {
			return true
		}
	}
	return false
}
