// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"fmt"
	"os/exec"

	dockerClientLib "github.com/docker/docker/client"
)

type lookPathFunc func(file string) (string, error)

func verifyExecutableInPath(name string, lookPath lookPathFunc) error {
	if _, err := lookPath(name); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Returns an error if the given runtime dependency / executable isn't found in PATH.
func EnsureRuntimeDependencyInstalled(_ context.Context, name string) error {
	if err := verifyExecutableInPath(name, exec.LookPath); err != nil {
		return fmt.Errorf("runtime dependency unavailable: %w", err)
	}
	return nil
}

// Returns an error if the Docker daemon isn't reachable. K3D and the containerized
// KubePrometheus build talk to the daemon via its API - a binary in PATH isn't enough.
func EnsureDockerDaemonReachable(ctx context.Context) error {
	dockerClient, err := dockerClientLib.NewClientWithOpts(
		dockerClientLib.FromEnv,
		dockerClientLib.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("creating Docker client: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()

	if _, err := dockerClient.Ping(ctx); err != nil {
		return fmt.Errorf("docker daemon unreachable (is Docker installed and running?): %w", err)
	}
	return nil
}
