// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package containerruntime

import "context"

// ImagePullPolicy controls when container images are pulled from the registry.
type ImagePullPolicy string

const (
	// ImagePullPolicyAlways always pulls the image from the registry.
	ImagePullPolicyAlways ImagePullPolicy = "Always"

	// ImagePullPolicyIfNotPresent pulls the image only if it is not already present locally.
	ImagePullPolicyIfNotPresent ImagePullPolicy = "IfNotPresent"

	// ImagePullPolicyNever never pulls the image; it must already exist locally.
	ImagePullPolicyNever ImagePullPolicy = "Never"
)

type ContainerRuntime interface {
	// Ensures that the given Docker network exists.
	CreateNetwork(ctx context.Context, name string)

	// Ensures that the given image is available, respecting the pull policy.
	PullImage(ctx context.Context, ref string, policy ImagePullPolicy)

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
