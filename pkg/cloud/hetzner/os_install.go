// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"k8c.io/kubeone/pkg/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type (
	ActivateLinuxInstallResponseBody struct {
		Linux struct {
			Dist          string   `json:"dist"`
			Arch          int      `json:"arch"`
			Lang          string   `json:"lang"`
			Password      string   `json:"password"`
			AuthorizedKey []string `json:"authorized_key"`
			Active        bool     `json:"active"`
		} `json:"linux"`
	}

	ResetResponseBody struct {
		Reset struct {
			ServerIP string `json:"server_ip"`
			Type     string `json:"type"`
		} `json:"reset"`
	}
)

// InstallOSOnBareMetalServers installs Ubuntu on each Hetzner Bare Metal server in parallel.
// Servers that are already SSH-reachable are skipped (idempotency). Since each server's OS
// installation takes ~8-12 minutes regardless of others, processing them in parallel bounds
// total wall-clock time to that of the slowest single server.
func (h *Hetzner) InstallOSOnBareMetalServers(ctx context.Context) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	privateKey := hetznerConfig.SSHKeyPair.PrivateKey
	fingerprint := hetznerConfig.SSHKeyPair.Fingerprint
	distribution := hetznerConfig.BareMetal.InstallImage.Distribution

	// Collect all bare metal hosts.
	var hosts []*config.HetznerBareMetalHost

	if config.ControlPlaneInHetznerBareMetal() {
		hosts = append(hosts, hetznerConfig.ControlPlane.BareMetal.BareMetalHosts...)
	}
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		hosts = append(hosts, nodeGroup.BareMetalHosts...)
	}

	if len(hosts) == 0 {
		return
	}

	slog.InfoContext(ctx, "Installing OS on Hetzner Bare Metal servers in parallel",
		slog.Int("servers", len(hosts)),
	)

	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func(host *config.HetznerBareMetalHost) {
			defer wg.Done()
			h.installOSOnServer(ctx, host, distribution, fingerprint, privateKey)
		}(host)
	}
	wg.Wait()

	slog.InfoContext(ctx, "All Hetzner Bare Metal servers are ready")
}

// installOSOnServer runs the full install flow for a single server: idempotency check, activate
// Linux install, hardware reset, and wait for SSH reachability.
func (h *Hetzner) installOSOnServer(
	ctx context.Context,
	host *config.HetznerBareMetalHost,
	distribution, fingerprint, privateKey string,
) {
	serverCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-id", host.ServerID),
	})

	address := h.getHetznerBareMetalServerIP(serverCtx, host.ServerID)

	if h.isServerReachable(serverCtx, address, privateKey) {
		slog.InfoContext(serverCtx, "Server already reachable via SSH, skipping OS installation")
		return
	}

	slog.InfoContext(serverCtx, "Installing OS on server")

	h.activateLinuxInstallation(serverCtx, host.ServerID, distribution, fingerprint)
	h.resetServer(serverCtx, host.ServerID)
	h.waitForServerReachable(serverCtx, host.ServerID, address, privateKey)

	slog.InfoContext(serverCtx, "OS installation completed, server is reachable")
}

// activateLinuxInstallation activates a Linux installation for the given server via the Hetzner
// Robot API. The installation is queued and executed on the next server boot.
func (h *Hetzner) activateLinuxInstallation(
	ctx context.Context,
	serverID, distribution, fingerprint string,
) {
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"dist":             []string{distribution},
			"arch":             []string{"64"},
			"lang":             []string{"en"},
			"authorized_key[]": []string{fingerprint},
		}).
		Post(fmt.Sprintf("/boot/%s/linux", serverID))
	assert.AssertErrNil(ctx, err, "Failed activating Linux installation")
	assert.Assert(ctx,
		response.StatusCode() == http.StatusOK,
		"Failed activating Linux installation",
		slog.Any("response", response),
	)

	slog.InfoContext(ctx, "Activated Linux installation",
		slog.String("distribution", distribution),
	)
}

// resetServer triggers a hardware reset on the given server via the Hetzner Robot API.
func (h *Hetzner) resetServer(ctx context.Context, serverID string) {
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"type": []string{constants.HetznerRobotResetTypeHardware},
		}).
		Post(fmt.Sprintf("/reset/%s", serverID))
	assert.AssertErrNil(ctx, err, "Failed resetting server")
	assert.Assert(ctx,
		response.StatusCode() == http.StatusOK,
		"Failed resetting server",
		slog.Any("response", response),
	)

	slog.InfoContext(ctx, "Triggered hardware reset")
}

// waitForServerReachable polls via SSH until the server becomes reachable after OS installation.
func (h *Hetzner) waitForServerReachable(
	ctx context.Context,
	serverID, address, privateKey string,
) {
	deadline := time.Now().Add(constants.HetznerOSInstallMaxWaitTime)

	for {
		if h.isServerReachable(ctx, address, privateKey) {
			return
		}

		assert.Assert(ctx,
			time.Now().Before(deadline),
			"Timed out waiting for server to become reachable after OS installation",
			slog.String("server-id", serverID),
			slog.Duration("max-wait", constants.HetznerOSInstallMaxWaitTime),
		)

		slog.InfoContext(ctx, "Server not yet reachable after OS installation, will retry...",
			slog.Duration("interval", constants.HetznerOSInstallPollInterval),
		)
		time.Sleep(constants.HetznerOSInstallPollInterval)
	}
}

// isServerReachable attempts a single SSH connection to check if the server is reachable.
func (h *Hetzner) isServerReachable(ctx context.Context, address, privateKey string) bool {
	connection, err := ssh.NewConnection(ssh.NewConnector(ctx), ssh.Opts{
		Context: ctx,

		Hostname:   address,
		Port:       22,
		Username:   "root",
		PrivateKey: []byte(privateKey),

		Timeout: 5 * time.Second,
	})
	if err != nil {
		return false
	}
	connection.Close()
	return true
}
