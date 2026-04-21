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
	ActivateHRobotLinuxInstallationResponseBody struct {
		Linux struct {
			Dist          string   `json:"dist"`
			Lang          string   `json:"lang"`
			Password      string   `json:"password"`
			AuthorizedKey []string `json:"authorized_key"`
			Active        bool     `json:"active"`
		} `json:"linux"`
	}

	HRobotResetResponseBody struct {
		Reset struct {
			ServerIP string `json:"server_ip"`
			Type     string `json:"type"`
		} `json:"reset"`
	}
)

// InstallOSOnAllHBMS installs Ubuntu on each HBMS in parallel. HBMS that are already
// SSH-reachable are skipped (idempotency). Since each HBMS's OS installation takes ~8-12
// minutes regardless of others, processing them in parallel bounds total wall-clock time to
// that of the slowest single host.
func (h *Hetzner) InstallOSOnAllHBMS(ctx context.Context) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	privateKey := hetznerConfig.SSHKeyPair.PrivateKey
	fingerprint := hetznerConfig.SSHKeyPair.Fingerprint

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
		slog.String("distribution", constants.HBMSInstallDistributionLatestUbuntu),
	)

	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func(host *config.HetznerBareMetalHost) {
			defer wg.Done()
			h.installOSOnHBMS(ctx, host, fingerprint, privateKey)
		}(host)
	}
	wg.Wait()

	slog.InfoContext(ctx, "All Hetzner Bare Metal servers are ready")
}

// installOSOnHBMS runs the full install flow for a single HBMS: idempotency check, activate
// Linux install via HRobot, hardware reset via HRobot, and wait for SSH reachability.
func (h *Hetzner) installOSOnHBMS(
	ctx context.Context,
	host *config.HetznerBareMetalHost,
	fingerprint, privateKey string,
) {
	hbmsCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-id", host.ServerID),
	})

	address := h.getHetznerBareMetalServerIP(hbmsCtx, host.ServerID)

	if h.isHBMSReachable(hbmsCtx, address, privateKey) {
		slog.InfoContext(hbmsCtx, "HBMS already reachable via SSH, skipping OS installation")
		return
	}

	slog.InfoContext(hbmsCtx, "Installing OS on HBMS")

	h.activateHRobotLinuxInstallation(hbmsCtx, host.ServerID, fingerprint)
	h.resetHBMS(hbmsCtx, host.ServerID)
	h.waitForHBMSReachable(hbmsCtx, host.ServerID, address, privateKey)

	slog.InfoContext(hbmsCtx, "OS installation completed, HBMS is reachable")
}

// activateHRobotLinuxInstallation activates a Linux installation for the given HBMS via the
// HRobot API, pinned to the latest Ubuntu LTS for security patch currency. The installation
// is queued and executed on the next boot.
func (h *Hetzner) activateHRobotLinuxInstallation(
	ctx context.Context,
	serverID, fingerprint string,
) {
	distribution := constants.HBMSInstallDistributionLatestUbuntu

	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"dist":             []string{distribution},
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

// resetHBMS triggers a hardware reset on the given HBMS via the HRobot API.
func (h *Hetzner) resetHBMS(ctx context.Context, serverID string) {
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"type": []string{constants.HRobotResetTypeHardware},
		}).
		Post(fmt.Sprintf("/reset/%s", serverID))
	assert.AssertErrNil(ctx, err, "Failed resetting HBMS")
	assert.Assert(ctx,
		response.StatusCode() == http.StatusOK,
		"Failed resetting HBMS",
		slog.Any("response", response),
	)

	slog.InfoContext(ctx, "Triggered hardware reset")
}

// waitForHBMSReachable polls via SSH until the HBMS becomes reachable after OS installation.
func (h *Hetzner) waitForHBMSReachable(
	ctx context.Context,
	serverID, address, privateKey string,
) {
	deadline := time.Now().Add(constants.HBMSOSInstallationMaxWaitTime)

	for {
		if h.isHBMSReachable(ctx, address, privateKey) {
			return
		}

		assert.Assert(ctx,
			time.Now().Before(deadline),
			"Timed out waiting for HBMS to become reachable after OS installation",
			slog.String("server-id", serverID),
			slog.Duration("max-wait", constants.HBMSOSInstallationMaxWaitTime),
		)

		slog.InfoContext(ctx, "HBMS not yet reachable after OS installation, will retry...",
			slog.Duration("interval", constants.HBMSOSInstallationPollInterval),
		)
		time.Sleep(constants.HBMSOSInstallationPollInterval)
	}
}

// isHBMSReachable attempts a single SSH connection to check if the HBMS is reachable.
func (h *Hetzner) isHBMSReachable(ctx context.Context, address, privateKey string) bool {
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
