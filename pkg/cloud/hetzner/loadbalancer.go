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
		existing, err = h.ensureExistingControlPlaneLB(ctx, existing, clusterName, network)
		if err != nil {
			return nil, fmt.Errorf("ensuring existing control-plane LB: %w", err)
		}
		if enablePublicInterface {
			existing, err = h.SetControlPlaneLBPublicInterface(ctx, clusterName, true)
			if err != nil {
				return nil, fmt.Errorf("enabling public interface on existing LB: %w", err)
			}
		}
		slog.InfoContext(ctx, "Hetzner LB already exists")
		return existing, nil
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
	if response.StatusCode != http.StatusOK {
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
	return lb, nil
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
// call. Gated on hostname being configured — without a hostname the
// LB's public IP is the operator's only entry point and disabling it
// would lock them out.
func (h *Hetzner) DisableControlPlaneLBPublicInterface(ctx context.Context) error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	if hetznerConfig.HCloudVPNCluster == nil ||
		hetznerConfig.ControlPlane.HCloud.LoadBalancer.Endpoint == "" {
		return nil
	}
	_, err := h.SetControlPlaneLBPublicInterface(ctx, config.ParsedGeneralConfig.Cluster.Name, false)
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
