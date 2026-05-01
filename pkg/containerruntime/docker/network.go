// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package docker

import (
	"context"
	"log/slog"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/network"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Ensures that the given Docker network exists.
func (d *Docker) CreateNetwork(ctx context.Context, name string) {
	_, err := d.client.NetworkCreate(ctx, name, network.CreateOptions{})

	// The network already exists.
	if cerrdefs.IsConflict(err) {
		return
	}

	assert.AssertErrNil(ctx, err, "Failed creating Docker network", slog.String("name", name))
}
