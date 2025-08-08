package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/term"
)

type CommandOptions struct {
	Cmd       []string
	Env       []string
	HostPath  string
	MountPath string
}

func ExecuteDockerCommand(command CommandOptions) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion("1.49")) // Maximum supported API version is 1.49
	assert.AssertErrNil(ctx, err, "Failed to create docker client")

	bootstrapImage := fmt.Sprintf("%s:%s", constants.BootstrapImageName, constants.BootstrapImageTag)
	bootstrapImage = "kubeaid-bootstrap-script-dev:latest"

	networkName := fmt.Sprintf("k3d-%s", constants.FlagNameManagementClusterNameDefaultValue)
	createNetworkIfNotCreated(ctx, cli, networkName)

	binds := []string{"/var/run/docker.sock:/var/run/docker.sock"}
	// if no --configs-directory flag is passed use the defaults
	if command.HostPath == "" || command.MountPath == "" {
		command.HostPath, err = os.Getwd()
		assert.AssertErrNil(ctx, err, "Failed to get current working directory")
		command.MountPath = constants.MountDirectory

		binds = append(binds, fmt.Sprintf("%s:%s", command.HostPath, command.MountPath))
	} else {
		// if --configs-directory flag is passed
		binds = append(binds, fmt.Sprintf("%s:%s", command.HostPath, command.MountPath))

		// Also mount the parent dir of the custom configs directory - this will be used for k3d configs
		binds = append(binds, fmt.Sprintf("%s:%s", filepath.Dir(command.HostPath), filepath.Dir(command.MountPath)))
	}

	// Create container with options
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: bootstrapImage,
		Cmd:   command.Cmd,
		Env:   command.Env,
	}, &container.HostConfig{Binds: binds}, &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{networkName: {}}}, nil, "")

	if err != nil && strings.Contains(err.Error(), "No such image") {
		resp, err := cli.ImagePull(ctx, bootstrapImage, image.PullOptions{})
		assert.AssertErrNil(ctx, err, "Failed to pull the bootstrap image")

		defer resp.Close()

		termFd, isTerm := term.GetFdInfo(os.Stdout)
		err = jsonmessage.DisplayJSONMessagesStream(resp, os.Stdout, termFd, isTerm, nil)
		assert.AssertErrNil(ctx, err, "Failed to pull the bootstrap image")
		ExecuteDockerCommand(command)
		return
	}

	assert.AssertErrNil(ctx, err, "Failed to create bootstrap container")

	// Start the container
	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	assert.AssertErrNil(ctx, err, "Failed to start bootstrap container")

	showContainerLogs(ctx, cli, resp.ID)
}

func createNetworkIfNotCreated(ctx context.Context, cli *client.Client, networkName string) {
	// Check if the network exists
	networks, err := cli.NetworkList(ctx, network.ListOptions{})
	assert.AssertErrNil(ctx, err, "Failed to get the docker network list")

	exists := false
	for _, network := range networks {
		if network.Name == networkName {
			exists = true
			break
		}
	}

	if !exists {
		// Create the network
		networkOptions := network.CreateOptions{
			Driver: "bridge",
		}
		_, err = cli.NetworkCreate(ctx, networkName, networkOptions)
		assert.AssertErrNil(ctx, err, "Failed to create the network", networkName)
	}
}

func showContainerLogs(ctx context.Context, cli *client.Client, containerID string) {
	containerResp, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: true})
	assert.AssertErrNil(ctx, err, "Failed to get bootstrap container logs")
	defer containerResp.Close()

	reader := bufio.NewReader(containerResp)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		assert.AssertErrNil(ctx, err, "Failed to fetch bootstrap logs")
		fmt.Fprint(os.Stdout, line)
	}
}
