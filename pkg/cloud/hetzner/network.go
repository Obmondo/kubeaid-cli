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

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Creates the Hetzner Network, if it doesn't already exist.
// The Hetzner Network details are returned.
func (h *Hetzner) CreateNetwork(ctx context.Context) *hcloud.Network {
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	_, parsedHetznerNetworkCIDR, err := net.ParseCIDR(constants.HetznerNetworkCIDR)
	assert.AssertErrNil(ctx, err, "Failed parsing Hetzner Network CIDR",
		slog.String("cidr", constants.HetznerNetworkCIDR),
	)

	_, parsedHCloudServersSubnetCIDR, err := net.ParseCIDR(constants.HCloudServersSubnetCIDR)
	assert.AssertErrNil(ctx, err, "Failed parsing HCloud servers subnet CIDR",
		slog.String("cidr", constants.HCloudServersSubnetCIDR),
	)

	networkClient := h.hcloudClient.Network

	// Check whether the Hetzner Network already exists.
	// If yes, then we assume that it was created by KubeAid CLI, with the correct configuration.

	network, response, err := networkClient.Get(ctx, clusterName)
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed running Hetzner Network GET operation",
	)

	if network != nil {
		slog.InfoContext(ctx, "Hetzner Network already exists")
		return network
	}

	// The Hetzner Network doesn't exist.
	// So, let's create it.

	network, response, err = networkClient.Create(ctx, hcloud.NetworkCreateOpts{
		Name: clusterName,

		Labels: map[string]string{
			// REFER : https://github.com/syself/cluster-api-provider-hetzner/issues/762#issuecomment-2887786636.
			fmt.Sprintf("caph-cluster-%s", clusterName): "owned",
		},

		IPRange: parsedHetznerNetworkCIDR,

		Subnets: []hcloud.NetworkSubnet{
			// For the HCloud servers.
			{
				Type:        hcloud.NetworkSubnetTypeCloud,
				IPRange:     parsedHCloudServersSubnetCIDR,
				NetworkZone: hcloud.NetworkZoneEUCentral,
			},
		},

		ExposeRoutesToVSwitch: true,
	})
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusCreated)),
		"Failed creating Hetzner Network",
	)
	slog.InfoContext(ctx, "Created Hetzner Network")

	return network
}

// Attaches the given HCloud server to the given Hetzner Network.
func (h *Hetzner) AttachHCloudServerToNetwork(ctx context.Context, serverID, networkID int) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.Int("server-id", serverID), slog.Int("network-id", networkID),
	})

	_, response, err := h.hcloudClient.Server.AttachToNetwork(ctx,
		&hcloud.Server{ID: serverID},
		hcloud.ServerAttachToNetworkOpts{
			Network: &hcloud.Network{ID: networkID},
		},
	)

	// When the attachment already exists.
	if (err != nil) && strings.Contains(err.Error(), string(hcloud.ErrorCodeServerAlreadyAttached)) {
		slog.InfoContext(ctx, "Attachment already exists")
		return
	}

	// Otherwise, it must be created.
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusCreated)),
		"Failed attaching HCloud server to Hetzner Network",
		slog.Any("response", response),
	)
}
