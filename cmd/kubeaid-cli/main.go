package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/containerd/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	kubeaidCoreRoot "github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root"
	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func main() {
	// By default, parent's PersistentPreRun gets overridden by a child's PersistentPreRun.
	// We want to disable this overriding behaviour and chain all the PersistentPreRuns.
	// REFERENCE : https://github.com/spf13/cobra/pull/2044.
	cobra.EnableTraverseRunHooks = true

	rootCmd := createRootCommand()

	err := rootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

// Constructs the root command.
func createRootCommand() *cobra.Command {
	// The interface should be same as that of KubeAid core.
	rootCmd := kubeaidCoreRoot.RootCmd

	rootCmd.Use = "kubeaid-cli"

	// Proxy cluster and devenv subcommands to containerized KubeAid core.
	for _, subCommand := range rootCmd.Commands() {
		subCommandName := subCommand.Name()

		if (subCommandName != "cluster") && (subCommandName != "devenv") {
			continue
		}

		// Unset PersistentPreRun and Run,
		// for this subcommand, and subcommands of this subcommand.
		unsetRunners(subCommand)

		// Proxy command execution to containerized KubeAid Core.
		subCommand.PersistentPreRun = proxyRun
	}

	return rootCmd
}

// Unsets PersistentPreRun and Run,
// for the given command, as well as, its subcommands.
func unsetRunners(command *cobra.Command) {
	command.PersistentPreRun = nil
	command.Run = func(cmd *cobra.Command, args []string) {}

	for _, subCommand := range command.Commands() {
		unsetRunners(subCommand)
	}
}

func proxyRun(command *cobra.Command, args []string) {
	ctx := command.Context()

	// Determine management cluster name.
	managementClusterName, err := command.Flags().GetString(constants.FlagNameManagementClusterName)
	assert.AssertErrNil(ctx, err, "Failed getting management-cluster-name flag value")

	dockerCLI, err := client.NewClientWithOpts(
		client.WithHostFromEnv(),
		client.WithAPIVersionNegotiation(),
	)
	assert.AssertErrNil(ctx, err, "Failed creating docker CLI")

	// The KubeAid Core container should run in the same Docker network,
	// where the K3D management cluster is / will be running.
	// Create that Docker network, if it doesn't already exist.

	networkName := fmt.Sprintf("k3d-%s", managementClusterName)

	_, err = dockerCLI.NetworkCreate(ctx, networkName, network.CreateOptions{})
	if !errdefs.IsConflict(err) {
		assert.AssertErrNil(ctx, err,
			"Failed creating Docker network",
			slog.String("name", networkName),
		)
	}

	// Spin up KubeAid Core container,
	// proxying the command execution.

	slog.InfoContext(ctx, "Spinning up KubeAid Core container")

	workingDirectory, err := os.Getwd()
	assert.AssertErrNil(ctx, err, "Failed determining working directory")

	configsDirectory, err := command.Flags().GetString(constants.FlagNameConfigsDirectoy)
	assert.AssertErrNil(ctx, err, "Failed determining configs directory")

	containerCreateResponse, err := dockerCLI.ContainerCreate(ctx,
		&container.Config{
			Image: fmt.Sprintf("ghcr.io/obmondo/kubeaid-core:v%s", version.Version),

			// In case of the bare-metal provider, the user might need to provide YubiKey pin for SSH
			// authentication against bare-metal servers.
			// So, we need a pseudo-terminal for the container, and keep the standard input accessible
			// from the host.
			Tty:       true,
			OpenStdin: true,

			AttachStdout: true,
			AttachStderr: true,

			Cmd: os.Args[1:],
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode(networkName),

			Binds: []string{
				"/var/run/docker.sock:/var/run/docker.sock",

				fmt.Sprintf("%s/%s:/%s",
					workingDirectory, configsDirectory,
					strings.TrimPrefix(strings.TrimPrefix(configsDirectory, "../"), "./"),
				),
				fmt.Sprintf("%s/outputs:/outputs", workingDirectory),
			},

			AutoRemove: true,
		},
		&network.NetworkingConfig{},
		&v1.Platform{},
		"kubeaid-core",
	)
	assert.AssertErrNil(ctx, err, "Failed creating KubeAid Core container")

	containerID := containerCreateResponse.ID

	containerAttachResponse, err := dockerCLI.ContainerAttach(ctx, containerID,
		container.AttachOptions{
			Stdin: true,

			Stdout: true,
			Stderr: true,
			Stream: true,
		},
	)
	assert.AssertErrNil(ctx, err, "Failed attaching the KubeAid Core container to the host program")
	defer containerAttachResponse.Close()

	err = dockerCLI.ContainerStart(ctx, containerID, container.StartOptions{})
	assert.AssertErrNil(ctx, err, "Failed starting KubeAid Core container")

	// Stream container logs, to this host program's stdout and stderr.
	go func() {
		_, err := io.Copy(os.Stdout, containerAttachResponse.Reader)
		assert.AssertErrNil(ctx, err, "Failed streaming KubeAid Core container logs")
	}()

	// Wait until the KubeAid Core container exits naturally.
	// Or, if we receive a program termination signal, then we explicitly stop the container, which
	// then gets auto-removed.

	containerStatusChan, containerExecutionErrorChan := dockerCLI.ContainerWait(ctx,
		containerID, container.WaitConditionNotRunning,
	)

	terminationSignalChan := make(chan os.Signal, 1)
	signal.Notify(terminationSignalChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-containerExecutionErrorChan:
		slog.ErrorContext(ctx, "KubeAid Core container execution failed", logger.Error(err))
		os.Exit(1)

	case <-containerStatusChan:

	case <-terminationSignalChan:
		err := dockerCLI.ContainerStop(ctx, containerID, container.StopOptions{})
		assert.AssertErrNil(ctx, err, "Failed stopping the KubeAid Core container")
	}
}
