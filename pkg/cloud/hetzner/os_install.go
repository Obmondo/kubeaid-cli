// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

type HRobotResetResponseBody struct {
	Reset struct {
		ServerIP string `json:"server_ip"`
		Type     string `json:"type"`
	} `json:"reset"`
}

// BootAllHBMSIntoRescue boots each HBMS into the Hetzner rescue system in
// parallel, so kubeaid-cli can SSH in and scan disks for the storage plan.
// It does NOT install an OS — CAPH does the real installimage run later.
// HBMS already SSH-reachable are skipped (idempotency). A rescue boot is a
// hardware reset into a ramdisk Debian (1-2 min), so running the hosts in
// parallel bounds total wall-clock time to that of the slowest single host.
func (h *Hetzner) BootAllHBMSIntoRescue(ctx context.Context) error {
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
		return nil
	}

	slog.InfoContext(ctx, "Booting Hetzner bare-metal servers into rescue system in parallel",
		slog.Int("servers", len(hosts)),
	)

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, host := range hosts {
		wg.Add(1)
		go func(host *config.HetznerBareMetalHost) {
			defer wg.Done()
			if err := h.bootHBMSIntoRescue(ctx, host, fingerprint, privateKey); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(host)
	}
	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("rescue boot failed on %d Hetzner bare-metal server(s): %v", len(errs), errs)
	}

	slog.InfoContext(ctx, "All Hetzner Bare Metal servers are ready")
	return nil
}

// bootHBMSIntoRescue runs the rescue-boot flow for a single HBMS: idempotency
// check, arm the rescue system via HRobot (with the Robot SSH key selected),
// hardware reset so the host boots into rescue, and wait for SSH reachability.
func (h *Hetzner) bootHBMSIntoRescue(
	ctx context.Context,
	host *config.HetznerBareMetalHost,
	fingerprint, privateKey string,
) error {
	hbmsCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-id", host.ServerID),
	})

	address, err := h.getHetznerBareMetalServerIP(host.ServerID)
	if err != nil {
		return fmt.Errorf("server %s: %w", host.ServerID, err)
	}

	if h.isHBMSReachable(hbmsCtx, address, privateKey) {
		slog.InfoContext(hbmsCtx, "Hetzner bare-metal server already reachable via SSH, skipping rescue boot")
		return nil
	}

	slog.InfoContext(hbmsCtx, "Booting Hetzner bare-metal server into rescue system")

	// Arm the rescue system, then hardware-reset the host so it actually boots
	// into it — rescue is only armed for the next boot, so the reset is
	// required, not optional.
	if err := h.activateHRobotRescue(hbmsCtx, host.ServerID, fingerprint); err != nil {
		return fmt.Errorf("server %s: %w", host.ServerID, err)
	}
	if err := h.resetHBMS(hbmsCtx, host.ServerID); err != nil {
		return fmt.Errorf("server %s: %w", host.ServerID, err)
	}
	if err := h.waitForHBMSReachable(hbmsCtx, host.ServerID, address, privateKey); err != nil {
		return fmt.Errorf("server %s: %w", host.ServerID, err)
	}

	slog.InfoContext(hbmsCtx, "Hetzner bare-metal server booted into rescue system and reachable")
	return nil
}

// activateHRobotRescue arms the Linux rescue system for the given HBMS via the
// HRobot API, authorising the supplied Robot SSH-key fingerprint so kubeaid-cli
// can SSH in key-only (no password). Rescue is armed for the next boot; the
// caller must hardware-reset the host to actually enter it.
func (h *Hetzner) activateHRobotRescue(
	ctx context.Context,
	serverID, fingerprint string,
) error {
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"os":               []string{constants.HRobotRescueOSLinux},
			"authorized_key[]": []string{fingerprint},
		}).
		Post(fmt.Sprintf("/boot/%s/rescue", serverID))
	if err != nil {
		return fmt.Errorf("activating rescue system for server %s: %w", serverID, err)
	}
	if response.StatusCode() != http.StatusOK {
		return fmt.Errorf("activating rescue system for server %s: unexpected status %d", serverID, response.StatusCode())
	}

	slog.InfoContext(ctx, "Armed rescue system",
		slog.String("os", constants.HRobotRescueOSLinux),
	)
	return nil
}

// resetHBMS triggers a hardware reset on the given HBMS via the HRobot API.
func (h *Hetzner) resetHBMS(ctx context.Context, serverID string) error {
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"type": []string{constants.HRobotResetTypeHardware},
		}).
		Post(fmt.Sprintf("/reset/%s", serverID))
	if err != nil {
		return fmt.Errorf("resetting Hetzner bare-metal server %s: %w", serverID, err)
	}
	if response.StatusCode() != http.StatusOK {
		return fmt.Errorf("resetting Hetzner bare-metal server %s: unexpected status %d", serverID, response.StatusCode())
	}

	slog.InfoContext(ctx, "Triggered hardware reset")
	return nil
}

// waitForHBMSReachable polls via SSH until the HBMS becomes reachable after the rescue boot.
func (h *Hetzner) waitForHBMSReachable(
	ctx context.Context,
	serverID, address, privateKey string,
) error {
	deadline := time.Now().Add(constants.HBMSRescueBootMaxWaitTime)

	for {
		if h.isHBMSReachable(ctx, address, privateKey) {
			return nil
		}

		if !time.Now().Before(deadline) {
			return fmt.Errorf("timed out waiting for Hetzner bare-metal server %s to become reachable (max wait %v)", serverID, constants.HBMSRescueBootMaxWaitTime)
		}

		slog.InfoContext(ctx, "Hetzner bare-metal server not yet reachable after rescue boot, will retry...",
			slog.Duration("interval", constants.HBMSRescueBootPollInterval),
		)
		time.Sleep(constants.HBMSRescueBootPollInterval)
	}
}

// isHBMSReachable attempts a single SSH connection to check if the HBMS is reachable.
//
// Two-phase to keep the yubikey-touch UX sane:
//
//  1. Cheap TCP probe on port 22 — no auth, no signing, no card touch.
//     The vast majority of polls during the 8-15 min install land here
//     (rescue is up but sshd hasn't started yet, or the install hasn't
//     rebooted into the target OS yet), and showing a touch prompt
//     every 20s for failing connections would carpet the operator's
//     terminal.
//  2. Once TCP is open, route through the per-host sshPool: the touch
//     prompt fires on the FIRST successful open per host (cached for
//     reuse by later prereq-infra steps like generateStoragePlan);
//     subsequent polls / ops on the same host don't touch the card.
//
// Auth: the pool's opener supplies both the agent socket (yubikey-
// resident keys — operator's PrivateKey is empty in that case; see
// parser.hydrateSSHKeyPairFromAgent) AND the supplied PrivateKey bytes
// (file-based key path). Same shape as waitForNATGatewaySSH in
// server.go.
func (h *Hetzner) isHBMSReachable(ctx context.Context, address, privateKey string) bool {
	if !isTCPPortOpen(ctx, address, 22, 3*time.Second) {
		return false
	}

	_, err := h.sshPool.getOrOpen(ctx, address, privateKey,
		fmt.Sprintf("verify Hetzner bare-metal server at %s reachable via SSH", address),
	)
	return err == nil
}

// isTCPPortOpen returns true when a TCP connection to address:port
// succeeds within timeout. Cheap reachability check used to gate the
// (more expensive, yubikey-touching) SSH handshake — without it, every
// poll during the OS install would either prompt the operator for a
// touch or noisily fail the SSH handshake on a closed port.
func isTCPPortOpen(ctx context.Context, address string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprintf("%d", port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
