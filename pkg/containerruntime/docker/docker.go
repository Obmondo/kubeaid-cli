// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package docker

import (
	"context"

	"github.com/docker/docker/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/containerruntime"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type Docker struct {
	client *client.Client
}

func NewDocker(ctx context.Context) containerruntime.ContainerRuntime {
	client, err := client.NewClientWithOpts(
		client.WithHostFromEnv(),
		client.WithAPIVersionNegotiation(),
	)
	assert.AssertErrNil(ctx, err, "Failed creating docker CLI")

	d := &Docker{client}

	return d
}
