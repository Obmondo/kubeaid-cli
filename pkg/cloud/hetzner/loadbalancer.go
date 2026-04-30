// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"k8s.io/utils/ptr"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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
) *hcloud.LoadBalancer {
	if existing := h.getLB(ctx, clusterName); existing != nil {
		existing = h.ensureExistingControlPlaneLB(ctx, existing, clusterName, network)
		if enablePublicInterface {
			existing = h.SetControlPlaneLBPublicInterface(ctx, clusterName, true)
		}
		slog.InfoContext(ctx, "Hetzner LB already exists")
		return existing
	}

	_, response, err := h.hcloudClient.LoadBalancer.Create(ctx, hcloud.LoadBalancerCreateOpts{
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
	assertHCloudCall(ctx, err, response, http.StatusCreated, "Failed creating Hetzner LB")
	slog.InfoContext(ctx, "Created Hetzner LB")

	return h.waitForLB(ctx, clusterName, func(lb *hcloud.LoadBalancer) bool {
		if len(lb.PrivateNet) == 0 {
			return false
		}
		if !enablePublicInterface {
			return true
		}
		return lb.PublicNet.Enabled && lb.PublicNet.IPv4.IP != nil
	})
}

// SetControlPlaneLBPublicInterface ensures the LB's public interface
// matches enabled. Idempotent — no-op when already in the desired
// state. Used both during bootstrap (enable) and post-bootstrap
// finalize (disable) once NetBird routes the private endpoint.
func (h *Hetzner) SetControlPlaneLBPublicInterface(
	ctx context.Context, clusterName string, enabled bool,
) *hcloud.LoadBalancer {
	lb := h.getLB(ctx, clusterName)
	if lb == nil || lb.PublicNet.Enabled == enabled {
		return lb
	}

	var (
		response *hcloud.Response
		err      error
		ready    func(*hcloud.LoadBalancer) bool
		logMsg   string
	)
	if enabled {
		_, response, err = h.hcloudClient.LoadBalancer.EnablePublicInterface(ctx, lb)
		ready = func(lb *hcloud.LoadBalancer) bool {
			return lb.PublicNet.Enabled && lb.PublicNet.IPv4.IP != nil
		}
		logMsg = "Enabled public interface on Hetzner LB"
	} else {
		_, response, err = h.hcloudClient.LoadBalancer.DisablePublicInterface(ctx, lb)
		ready = func(lb *hcloud.LoadBalancer) bool { return !lb.PublicNet.Enabled }
		logMsg = "Disabled public interface on Hetzner LB"
	}
	assertHCloudCall(ctx, err, response, http.StatusCreated,
		fmt.Sprintf("Failed setting public interface (enabled=%t) on Hetzner LB", enabled))

	lb = h.waitForLB(ctx, clusterName, ready)
	slog.InfoContext(ctx, logMsg)
	return lb
}

// DisableControlPlaneLBPublicInterface is the post-bootstrap finalize
// call. Gated on hostname being configured — without a hostname the
// LB's public IP is the operator's only entry point and disabling it
// would lock them out.
func (h *Hetzner) DisableControlPlaneLBPublicInterface(ctx context.Context) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	if hetznerConfig.HCloudVPNCluster == nil ||
		hetznerConfig.ControlPlane.HCloud.LoadBalancer.Hostname == "" {
		return
	}
	h.SetControlPlaneLBPublicInterface(ctx, config.ParsedGeneralConfig.Cluster.Name, false)
}

func (h *Hetzner) ensureExistingControlPlaneLB(ctx context.Context,
	loadBalancer *hcloud.LoadBalancer,
	clusterName string,
	network *hcloud.Network,
) *hcloud.LoadBalancer {
	ownershipLabel := controlPlaneLBOwnershipLabel(clusterName)
	if loadBalancer.Labels[ownershipLabel] != "owned" {
		labels := map[string]string{}
		for key, value := range loadBalancer.Labels {
			labels[key] = value
		}
		labels[ownershipLabel] = "owned"

		updated, response, err := h.hcloudClient.LoadBalancer.Update(ctx, loadBalancer,
			hcloud.LoadBalancerUpdateOpts{Labels: labels},
		)
		assertHCloudCall(ctx, err, response, http.StatusOK, "Failed updating Hetzner LB labels")
		loadBalancer = updated
	}

	if !loadBalancerAttachedToNetwork(loadBalancer, network.ID) {
		_, response, err := h.hcloudClient.LoadBalancer.AttachToNetwork(ctx, loadBalancer,
			hcloud.LoadBalancerAttachToNetworkOpts{Network: network},
		)
		assertHCloudCall(ctx, err, response, http.StatusCreated, "Failed attaching Hetzner LB to network")
		return h.waitForLB(ctx, clusterName, func(lb *hcloud.LoadBalancer) bool {
			return loadBalancerAttachedToNetwork(lb, network.ID)
		})
	}

	return loadBalancer
}

func (h *Hetzner) getLB(ctx context.Context, clusterName string) *hcloud.LoadBalancer {
	lb, response, err := h.hcloudClient.LoadBalancer.Get(ctx, clusterName)
	assertHCloudCall(ctx, err, response, http.StatusOK, "Failed running Hetzner LB GET operation")
	return lb
}

func (h *Hetzner) waitForLB(ctx context.Context,
	clusterName string,
	ready func(*hcloud.LoadBalancer) bool,
) *hcloud.LoadBalancer {
	for {
		time.Sleep(10 * time.Second)
		if lb := h.getLB(ctx, clusterName); ready(lb) {
			return lb
		}
	}
}

// assertHCloudCall checks the (err, response, expected status) tuple
// the hcloud-go SDK returns for nearly every call, fataling with the
// supplied message on mismatch. Centralised so the seven call sites
// in this file don't repeat the same three-line conjunction.
func assertHCloudCall(ctx context.Context,
	err error, response *hcloud.Response, expectedStatus int, msg string,
) {
	assert.Assert(ctx,
		err == nil && response != nil && response.StatusCode == expectedStatus,
		msg,
		slog.Any("response", response),
	)
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
