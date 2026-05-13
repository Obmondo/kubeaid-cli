// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"k8s.io/utils/ptr"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

// DisableDeletionProtection disables deletion protection on the KubeAPI LB and
// NAT Gateway server so that CAPH can delete them during cluster teardown.
func (h *Hetzner) DisableDeletionProtection(ctx context.Context) error {
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	// Disable deletion protection on the KubeAPI LB.
	lb, response, err := h.loadBalancerClient.Get(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("looking up Hetzner LB for deletion protection disable: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("looking up Hetzner LB: unexpected status %d", response.StatusCode)
	}
	if lb != nil && lb.Protection.Delete {
		_, response, err = h.loadBalancerClient.ChangeProtection(ctx, lb,
			hcloud.LoadBalancerChangeProtectionOpts{Delete: ptr.To(false)},
		)
		if err != nil {
			return fmt.Errorf("disabling deletion protection on Hetzner LB: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("disabling deletion protection on Hetzner LB: unexpected status %d", response.StatusCode)
		}
		slog.InfoContext(ctx, "Disabled deletion protection on Hetzner LB")
	}

	// Disable deletion protection on the NAT Gateway server.
	serverName := fmt.Sprintf("%s-nat-gateway", clusterName)
	server, response, err := h.serverClient.GetByName(ctx, serverName)
	if err != nil {
		return fmt.Errorf("looking up NAT Gateway server for deletion protection disable: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("looking up NAT Gateway server: unexpected status %d", response.StatusCode)
	}
	if server != nil && server.Protection.Delete {
		// Hetzner requires Delete and Rebuild protection flags to be
		// sent together (`'delete' and 'rebuild' field required to be
		// the same value`), so toggle both off.
		_, response, err = h.serverClient.ChangeProtection(ctx, server,
			hcloud.ServerChangeProtectionOpts{
				Delete:  ptr.To(false),
				Rebuild: ptr.To(false),
			},
		)
		if err != nil {
			return fmt.Errorf("disabling deletion protection on NAT Gateway server: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("disabling deletion protection on NAT Gateway server: unexpected status %d", response.StatusCode)
		}
		slog.InfoContext(ctx, "Disabled deletion protection on NAT Gateway server")
	}

	return nil
}
