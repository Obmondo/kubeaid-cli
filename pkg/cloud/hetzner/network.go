// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

// CreateNetwork creates the Hetzner Network, if it doesn't already exist.
func (h *Hetzner) CreateNetwork(ctx context.Context) (*hcloud.Network, error) {
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	hetznerNetwork := config.ParsedGeneralConfig.Cloud.Hetzner.HCloud.HetznerNetwork

	_, parsedHetznerNetworkCIDR, err := net.ParseCIDR(hetznerNetwork.CIDR)
	if err != nil {
		return nil, fmt.Errorf("parsing Hetzner Network CIDR %q: %w", hetznerNetwork.CIDR, err)
	}

	_, parsedHCloudServersSubnetCIDR, err := net.ParseCIDR(hetznerNetwork.HCloudServersSubnetCIDR)
	if err != nil {
		return nil, fmt.Errorf("parsing HCloud servers subnet CIDR %q: %w", hetznerNetwork.HCloudServersSubnetCIDR, err)
	}

	network, response, err := h.networkClient.Get(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("running Hetzner Network GET operation: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("running Hetzner Network GET operation: unexpected status %d", response.StatusCode)
	}

	if network != nil {
		slog.InfoContext(ctx, "Hetzner Network already exists")
		return network, nil
	}

	network, response, err = h.networkClient.Create(ctx, hcloud.NetworkCreateOpts{
		Name: clusterName,

		Labels: map[string]string{
			// REFER : https://github.com/syself/cluster-api-provider-hetzner/issues/762#issuecomment-2887786636.
			fmt.Sprintf("caph-cluster-%s", clusterName): "owned",
		},

		IPRange: parsedHetznerNetworkCIDR,

		Subnets: []hcloud.NetworkSubnet{
			{
				Type:        hcloud.NetworkSubnetTypeCloud,
				IPRange:     parsedHCloudServersSubnetCIDR,
				NetworkZone: hcloud.NetworkZoneEUCentral,
			},
		},

		ExposeRoutesToVSwitch: true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating Hetzner Network: %w", err)
	}
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("creating Hetzner Network: unexpected status %d", response.StatusCode)
	}
	slog.InfoContext(ctx, "Created Hetzner Network")

	return network, nil
}

// AttachHCloudServerToNetwork attaches the given HCloud server to the given Hetzner Network.
func (h *Hetzner) AttachHCloudServerToNetwork(ctx context.Context, serverID, networkID int) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.Int("server-id", serverID), slog.Int("network-id", networkID),
	})

	_, response, err := h.serverClient.AttachToNetwork(ctx,
		&hcloud.Server{ID: serverID},
		hcloud.ServerAttachToNetworkOpts{
			Network: &hcloud.Network{ID: networkID},
		},
	)

	if (err != nil) && strings.Contains(err.Error(), string(hcloud.ErrorCodeServerAlreadyAttached)) {
		slog.InfoContext(ctx, "Attachment already exists")
		return nil
	}

	if err != nil {
		return fmt.Errorf("attaching HCloud server %d to Hetzner Network %d: %w", serverID, networkID, err)
	}
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("attaching HCloud server %d to Hetzner Network %d: unexpected status %d", serverID, networkID, response.StatusCode)
	}

	return nil
}
