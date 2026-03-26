// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/containerruntime"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (d *Docker) RunContainer(ctx context.Context, options containerruntime.RunContainerOptions) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("container", options.Name),
	})

	slog.InfoContext(ctx, "Running container")

	binds := []string{}
	for hostPath, mountPath := range options.Binds {
		binds = append(binds,
			fmt.Sprintf("%s:%s", hostPath, mountPath))
	}

	containerCreateResponse, err := d.client.ContainerCreate(ctx,
		&container.Config{
			Image: options.ImageRef,

			// To make the YubiKey support work, we need a pseudo-terminal for the container, and keep
			// the standard input accessible from the host.
			Tty:       true,
			OpenStdin: true,

			AttachStdout: true,
			AttachStderr: true,

			Env: options.Envs,

			Cmd: options.Command,
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode(options.Network),
			Binds:       binds,
			AutoRemove:  true,
		},
		&network.NetworkingConfig{},
		&v1.Platform{},
		options.Name,
	)
	assert.AssertErrNil(ctx, err, "Failed creating container")

	containerID := containerCreateResponse.ID

	containerAttachResponse, err := d.client.ContainerAttach(ctx, containerID,
		container.AttachOptions{
			Stdin: true,

			Stdout: true,
			Stderr: true,
			Stream: true,
		},
	)
	assert.AssertErrNil(ctx, err, "Failed attaching container to the host program")
	defer containerAttachResponse.Close()

	err = d.client.ContainerStart(ctx, containerID, container.StartOptions{})
	assert.AssertErrNil(ctx, err, "Failed starting container")

	// Stream container logs, to this host program's stdout and stderr.
	go func() {
		_, err := io.Copy(os.Stdout, containerAttachResponse.Reader)
		assert.AssertErrNil(ctx, err, "Failed streaming container logs")
	}()

	// Wait until the container exits naturally.
	// Or, if we've receive a program termination signal, we explicitly stop the container,
	// which then gets auto-removed.

	containerStatusChan, containerExecutionErrorChan := d.client.ContainerWait(ctx,
		containerID, container.WaitConditionNotRunning,
	)

	terminationSignalChan := make(chan os.Signal, 1)
	signal.Notify(terminationSignalChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-containerExecutionErrorChan:
		assert.AssertErrNil(ctx, err, "Container execution failed")

	case <-containerStatusChan:

	case <-terminationSignalChan:
		err := d.client.ContainerStop(ctx, containerID, container.StopOptions{})
		assert.AssertErrNil(ctx, err, "Failed stopping container")
	}
}
