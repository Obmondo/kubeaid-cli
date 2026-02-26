// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns IDs of the HCloud servers associated with the given Kubernetes cluster which was
// provisioned using Cluster API Provider Hetzner (CAPH).
func (h *Hetzner) GetHCloudServerIDsForCluster(ctx context.Context, name string) []int {
	server := h.hcloudClient.Server

	// Suppose the Kubernetes cluster name is vpn.
	// Since it was provisioned using Cluster API Provider Hetzner (CAPH), the associated HCloud
	// servers must have the "caph-cluster-vpn: owned" label attached.
	// So, we'll list those HCloud servers, which have the following label attached.
	servers, response, err := server.List(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: "caph-cluster-" + name,
		},
	})
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed listing HCloud servers associated with the given Kubernetes cluster",
		slog.String("cluster", name),
	)

	serverIDs := []int{}
	for _, server := range servers {
		serverIDs = append(serverIDs, server.ID)
	}
	return serverIDs
}
