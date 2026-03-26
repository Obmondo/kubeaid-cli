// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package containerruntime

import "context"

type ContainerRuntime interface {
	// Ensures that the given Docker network exists.
	CreateNetwork(ctx context.Context, name string)

	// Ensures that the given image is pulled.
	PullImage(ctx context.Context, ref string)

	// Runs the given container, streaming logs to the stdout and stderr.
	// NOTE : Blocks the current thread.
	RunContainer(ctx context.Context, options RunContainerOptions)

	// Closes connection to the Container Runtime's socket.
	CloseSocketConnection(ctx context.Context)
}

type RunContainerOptions struct {
	Name,
	ImageRef,

	Network string

	Binds map[string]string
	Envs  []string

	Command []string
}
