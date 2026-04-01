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

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Creates the Hetzner LB, if it doesn't already exist.
// The Hetzner LB details are returned.
func (h *Hetzner) CreateLB(ctx context.Context,
	clusterName string,
	network *hcloud.Network,
	location string,
) *hcloud.LoadBalancer {
	loadBalancer, response, err := h.hcloudClient.LoadBalancer.Get(ctx, clusterName)
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed running Hetzner LB GET operation",
		slog.Any("response", response),
	)
	if loadBalancer != nil {
		slog.InfoContext(ctx, "Hetzner LB already exists")
		return loadBalancer
	}

	_, response, err = h.hcloudClient.LoadBalancer.Create(ctx, hcloud.LoadBalancerCreateOpts{
		Name: clusterName,
		LoadBalancerType: &hcloud.LoadBalancerType{
			Name:        constants.HCloudLBTypeLB11,
			Description: fmt.Sprintf("LB in front of the Kubernetes API server for %s cluster", clusterName),
		},

		Location:        &hcloud.Location{Name: location},
		PublicInterface: ptr.To(false),
		Network:         network,

		Labels: map[string]string{
			// REFER : https://github.com/syself/cluster-api-provider-hetzner/issues/762#issuecomment-2887786636.
			fmt.Sprintf("caph-cluster-%s", clusterName): "owned",
		},
	})
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusCreated)),
		"Failed creating Hetzner LB",
		slog.Any("response", response),
	)
	slog.InfoContext(ctx, "Created Hetzner LB")

	// The private IP allocation isn't instant.
	// So we need to wait fot sometime and GET the loadbalancer.
	for {
		time.Sleep(10 * time.Second)

		loadBalancer, response, err = h.hcloudClient.LoadBalancer.Get(ctx, clusterName)
		assert.Assert(ctx,
			((err == nil) && (response.StatusCode == http.StatusOK)),
			"Failed running Hetzner LB GET operation",
			slog.Any("response", response),
		)

		if len(loadBalancer.PrivateNet) > 0 {
			break
		}
	}

	return loadBalancer
}
