// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package containerd

import (
	"context"
	"log/slog"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// RunContainerArgs holds the arguments for running a container via containerd.
type RunContainerArgs struct {
	// Container image reference (e.g. ghcr.io/obmondo/kubeaid-core:v0.20.0).
	ImageRef string

	// Command to execute inside the container.
	Command []string

	// Bind mounts: host path -> container path.
	Binds map[string]string

	// Environment variables.
	Env []string
}

// RunContainer pulls an image and runs a container via containerd.
// It blocks until the container exits and returns the exit code.
func RunContainer(ctx context.Context, args RunContainerArgs) uint32 {
	ctx = namespaces.WithNamespace(ctx, constants.ContainerdNamespace)

	slog.InfoContext(ctx, "Connecting to containerd",
		slog.String("socket", constants.ContainerdSocketPath),
	)

	client, err := containerd.New(constants.ContainerdSocketPath)
	assert.AssertErrNil(ctx, err, "Failed connecting to containerd")
	defer func() {
		if err := client.Close(); err != nil {
			slog.WarnContext(ctx, "Failed closing containerd client",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Pull image.
	slog.InfoContext(ctx, "Pulling container image",
		slog.String("image", args.ImageRef),
	)

	image, err := client.Pull(ctx, args.ImageRef,
		containerd.WithPullUnpack,
	)
	assert.AssertErrNil(ctx, err, "Failed pulling container image")

	// Build OCI spec with mounts and env.
	containerID := "kubeaid-core"
	snapshotID := containerID + "-snapshot"

	ociOpts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithProcessArgs(args.Command...),
	}

	if len(args.Env) > 0 {
		ociOpts = append(ociOpts, oci.WithEnv(args.Env))
	}

	// Add bind mounts.
	var mounts []specs.Mount
	for hostPath, containerPath := range args.Binds {
		mounts = append(mounts, specs.Mount{
			Type:        "bind",
			Source:      hostPath,
			Destination: containerPath,
			Options:     []string{"rbind", "rw"},
		})
	}
	if len(mounts) > 0 {
		ociOpts = append(ociOpts, oci.WithMounts(mounts))
	}

	// Create container.
	slog.InfoContext(ctx, "Creating container",
		slog.String("id", containerID),
	)

	container, err := client.NewContainer(ctx, containerID,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(snapshotID, image),
		containerd.WithNewSpec(ociOpts...),
	)
	assert.AssertErrNil(ctx, err, "Failed creating container")
	defer func() {
		if err := container.Delete(ctx,
			containerd.WithSnapshotCleanup,
		); err != nil {
			slog.WarnContext(ctx, "Failed cleaning up container",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Create and start the task (running process).
	task, err := container.NewTask(ctx,
		cio.NewCreator(cio.WithStdio),
	)
	assert.AssertErrNil(ctx, err, "Failed creating container task")
	defer func() {
		if _, err := task.Delete(ctx); err != nil {
			slog.WarnContext(ctx, "Failed deleting container task",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Wait must be called before Start to avoid race conditions.
	exitStatusC, err := task.Wait(ctx)
	assert.AssertErrNil(ctx, err, "Failed setting up task wait")

	slog.InfoContext(ctx, "Starting container")

	err = task.Start(ctx)
	assert.AssertErrNil(ctx, err, "Failed starting container")

	// Wait for container to finish.
	status := <-exitStatusC
	exitCode, _, err := status.Result()
	assert.AssertErrNil(ctx, err, "Failed getting container exit status")

	slog.InfoContext(ctx, "Container exited",
		slog.Any("exit-code", exitCode),
	)

	return exitCode
}
