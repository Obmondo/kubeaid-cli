// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

// ProvisionPrerequisiteInfrastructure provisions infrastructure required before CAPH starts
// spinning up the cluster.
//
//nolint:gocognit,nestif
func (h *Hetzner) ProvisionPrerequisiteInfrastructure(ctx context.Context) error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	bar := progress.FromCtx(ctx)

	// SSH connections opened during this phase (isHBMSReachable's
	// post-install probe, generateStoragePlan's disk scan) live in
	// h.sshPool; the same authenticated channel is reused across ops
	// targeting the same host. Reclaim them on every exit path —
	// success, error, panic — so we don't leak goroutines / file
	// descriptors past the phase boundary.
	defer h.sshPool.closeAll()

	if config.UsingHetznerBareMetal() {
		sshKeyPair := hetznerConfig.SSHKeyPair
		if err := h.CreateHetznerBareMetalSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig); err != nil {
			return fmt.Errorf("creating Hetzner Bare Metal SSH key: %w", err)
		}
		bar.Substep("Registered Hetzner Bare Metal SSH key")

		// OS install is the longest single step in the bare-metal
		// path: 8-12 minutes per server in parallel. Surface a
		// transient "installing…" line under the spinner so the
		// operator can see why the bar's been sitting at the same
		// step for several minutes; the count is rendered up front
		// so they have a sense of the wall-clock floor.
		bmHostCount := countBareMetalHosts(hetznerConfig)
		releaseInstall := bar.InProgress(fmt.Sprintf(
			"Installing OS on %d bare-metal server(s) (~8-15 min per server typical, up to 20 min, in parallel)",
			bmHostCount,
		))
		err := h.InstallOSOnAllHBMS(ctx)
		releaseInstall()
		if err != nil {
			return fmt.Errorf("installing OS on Hetzner bare-metal servers: %w", err)
		}
		bar.Substep(fmt.Sprintf("Installed OS on %d bare-metal server(s)", bmHostCount))

		if err := h.GenerateStoragePlans(ctx, hetznerConfig); err != nil {
			return fmt.Errorf("generating storage plans: %w", err)
		}

		hydrateNodeGroupLabels(hetznerConfig)
	}

	// HCloud Network is only created for modes that actually host
	// HCloud servers (hcloud, hybrid). Pure bare-metal stays nil here
	// — the vSwitch is the only L2 fabric the cluster needs, and
	// there's no HCloud Network to attach it to.
	var network *hcloud.Network
	if config.UsingHCloud() {
		n, err := h.CreateNetwork(ctx)
		if err != nil {
			return fmt.Errorf("creating Hetzner Network: %w", err)
		}
		network = n
		bar.Substep("Created Hetzner Network")

		sshKeyPair := hetznerConfig.SSHKeyPair
		if err := h.CreateHCloudSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig); err != nil {
			return fmt.Errorf("creating HCloud SSH key: %w", err)
		}
		bar.Substep("Registered HCloud SSH key")

		// Create the NAT gateway BEFORE the LB pre-create + DNS-wait
		// block. NAT only depends on the Hetzner network + SSH key,
		// not on the LB or on DNS resolution; running it earlier means
		// the operator isn't watching the spinner spend an extra
		// minute provisioning NAT after they've finished pasting DNS
		// A records.
		//
		// Skipped for the single-node public control-plane topology: the
		// lone node egresses over its own public IPv4 — no NAT needed.
		if !config.HCloudSingleNodePublic() {
			if err := h.CreateNATGateway(ctx, network.ID); err != nil {
				return fmt.Errorf("creating NAT gateway: %w", err)
			}
		}

		// Pre-create the control-plane LB. The DNS-wait it triggers is
		// the longest blocking step in this phase (operator-driven —
		// they go to their DNS provider and add A records), so it
		// always sits last in the prerequisite block.
		// Two cases need a pre-created LB:
		//   - cluster.type=vpn: this cluster IS the VPN; its apiserver
		//     sits on a public LB so external clients can bootstrap
		//     NetBird. The CoreDNS hosts ConfigMap also depends on the
		//     LB IPs being known at template-render time.
		//   - HCloudVPNCluster set (workload connecting to existing VPN):
		//     a private LB sits behind NetBird; the CAPI HCloudCluster
		//     manifest references its IPs via globals.
		// Workload clusters not on VPN don't need pre-creation — CAPI
		// handles LB lifecycle for them.
		if h.shouldPreCreateControlPlaneLB() {
			if err := h.preCreateControlPlaneLB(ctx, network); err != nil {
				return err
			}
		}

		// Provision the NetBird Coturn Floating IP on a multi-CP VPN
		// cluster: with more than one control-plane node the active
		// Coturn (host-network STUN/TURN) can land on any of them, so
		// its public IP must float. hcloud-fip-controller reassigns it
		// across nodes and the capi-cluster chart binds it on each CP
		// via netplan; here we only allocate the (unassigned) IP and
		// stash it so it threads into the CAPI cluster values.
		if config.CoturnFloatingIPEnabled() {
			floatingIP, err := h.CreateCoturnFloatingIP(ctx,
				config.ParsedGeneralConfig.Cluster.Name,
				hetznerConfig.ControlPlane.Regions[0],
			)
			if err != nil {
				return fmt.Errorf("creating Coturn Floating IP: %w", err)
			}
			globals.CoturnFloatingIPs = []string{floatingIP}
			bar.Substep("Created NetBird Coturn Floating IP")
		}
	}

	if config.UsingHetznerBareMetal() {
		vswitchID, err := h.CreateVSwitch(ctx)
		if err != nil {
			return fmt.Errorf("creating VSwitch: %w", err)
		}
		bar.Substep("Created Hetzner Bare Metal vSwitch")

		// vSwitch ↔ HCloud Network only matters when there's a
		// Network — i.e. hybrid mode. Pure bare-metal stops at the
		// standalone vSwitch; the L2 between BM servers is enough,
		// and there's no HCloud side to bridge to.
		if network != nil {
			if err := h.ConnectVSwitchWithHetznerNetwork(ctx, network); err != nil {
				return fmt.Errorf("connecting VSwitch with Hetzner Network: %w", err)
			}
			bar.Substep("Connected vSwitch to Hetzner Network")
		}

		bmHostCount := countBareMetalHosts(hetznerConfig)
		releaseAttach := bar.InProgress(fmt.Sprintf(
			"Attaching %d bare-metal server(s) to vSwitch", bmHostCount,
		))
		attachErr := attachAllBareMetalServersToVSwitch(ctx, h, hetznerConfig, vswitchID)
		releaseAttach()
		if attachErr != nil {
			return attachErr
		}
		bar.Substep(fmt.Sprintf("Attached %d bare-metal server(s) to vSwitch", bmHostCount))
	}

	// Workload-cluster-connecting-to-VPN: attach the existing VPN
	// cluster's servers to this cluster's network so they share L2.
	// LB creation for this case happened above in preCreateControlPlaneLB.
	// HCloudVPNCluster only exists in hcloud / hybrid modes (cross-validated
	// in pkg/config/parser/validate.go), so network is guaranteed non-nil
	// here.
	if hetznerConfig.HCloudVPNCluster != nil {
		serverIDs, err := h.GetHCloudServerIDsForCluster(ctx,
			hetznerConfig.HCloudVPNCluster.Name,
		)
		if err != nil {
			return fmt.Errorf("getting VPN cluster server IDs: %w", err)
		}
		for _, serverID := range serverIDs {
			if err := h.AttachHCloudServerToNetwork(ctx, serverID, network.ID); err != nil {
				return fmt.Errorf("attaching HCloud server %d to network: %w", serverID, err)
			}
		}
		if len(serverIDs) > 0 {
			bar.Substep(fmt.Sprintf("Attached %d existing servers to network", len(serverIDs)))
		}
	}

	return nil
}

// countBareMetalHosts counts every bare-metal host declared on the
// Hetzner config — both the CP block (when control-plane is on BM)
// and every BM node-group. Used to label progress-bar lines so the
// operator knows how many servers are involved in a parallel step.
func countBareMetalHosts(hetznerConfig *config.HetznerConfig) int {
	count := 0
	if config.ControlPlaneInHetznerBareMetal() {
		count += len(hetznerConfig.ControlPlane.BareMetal.BareMetalHosts)
	}
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		count += len(nodeGroup.BareMetalHosts)
	}
	return count
}

// attachAllBareMetalServersToVSwitch collects every bare-metal host
// (CP + all BM node-groups) and attaches them to vswitchID in a single
// batch request. Robot's POST /vswitch/{id}/server takes an array and
// applies it atomically, so one call attaches every server without the
// per-server VSWITCH_IN_PROCESS race a loop would hit.
func attachAllBareMetalServersToVSwitch(
	ctx context.Context,
	h *Hetzner,
	hetznerConfig *config.HetznerConfig,
	vswitchID int,
) error {
	var serverIDs []string
	if config.ControlPlaneInHetznerBareMetal() {
		for _, host := range hetznerConfig.ControlPlane.BareMetal.BareMetalHosts {
			serverIDs = append(serverIDs, host.ServerID)
		}
	}
	for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
		for _, host := range nodeGroup.BareMetalHosts {
			serverIDs = append(serverIDs, host.ServerID)
		}
	}

	if err := h.AttachServersToVSwitch(ctx, serverIDs, vswitchID); err != nil {
		return fmt.Errorf("attaching bare-metal servers to VSwitch: %w", err)
	}
	return nil
}

// shouldPreCreateControlPlaneLB reports whether kubeaid-cli should
// provision the control-plane LB itself before CAPI runs. True for:
//   - cluster.type=vpn — this cluster IS the VPN; CoreDNS hosts
//     ConfigMap and the operator's DNS A-record both need the LB IPs
//     known at template-render time.
//   - HCloudVPNCluster set — workload connecting to an existing VPN;
//     CAPI HCloudCluster manifest references the pre-created LB by IP.
//
// False otherwise: workload-not-on-VPN clusters let CAPI handle LB
// lifecycle on its own, and the single-node public control-plane
// topology has no control-plane LB at all.
func (h *Hetzner) shouldPreCreateControlPlaneLB() bool {
	if config.HCloudSingleNodePublic() {
		return false
	}
	cluster := config.ParsedGeneralConfig.Cluster
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	return cluster.Type == constants.ClusterTypeVPN ||
		hetznerConfig.HCloudVPNCluster != nil
}

// preCreateControlPlaneLB creates the control-plane LB and populates
// globals.ControlPlaneLB* used by template rendering. When the
// loadBalancer endpoint hostname is configured, the LB has a public IP
// and we wait for the operator's DNS A record to land on it before
// returning — without that wait, ACME HTTP-01 fails as soon as
// Keycloak's Ingress syncs and recovery is messy.
func (h *Hetzner) preCreateControlPlaneLB(ctx context.Context, network *hcloud.Network) error {
	bar := progress.FromCtx(ctx)
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	controlPlaneHostname := hetznerConfig.ControlPlane.HCloud.LoadBalancer.Endpoint

	loadBalancer, err := h.CreateLB(ctx,
		config.ParsedGeneralConfig.Cluster.Name,
		network,
		hetznerConfig.ControlPlane.HCloud.LoadBalancer.Region,
		controlPlaneHostname != "",
	)
	if err != nil {
		return fmt.Errorf("creating control-plane LB: %w", err)
	}
	bar.Substep("Created control-plane Load Balancer")

	globals.ControlPlaneLBPrivateIP = loadBalancer.PrivateNet[0].IP.String()
	globals.ControlPlaneHostname = controlPlaneHostname

	if controlPlaneHostname != "" {
		assert.Assert(ctx,
			loadBalancer.PublicNet.Enabled && loadBalancer.PublicNet.IPv4.IP != nil,
			"Control-plane LB hostname requires a bootstrap public LB IP, but the LB has no public IPv4",
			slog.String("hostname", controlPlaneHostname),
		)
		globals.ControlPlaneLBBootstrapPublicIP = loadBalancer.PublicNet.IPv4.IP.String()

		bar.Substep("Waiting for control-plane DNS")
		if err := waitForControlPlaneDNS(ctx, globals.ControlPlaneLBBootstrapPublicIP); err != nil {
			return fmt.Errorf("waiting for control-plane DNS: %w", err)
		}
	}
	return nil
}

// waitForControlPlaneDNS pauses bootstrap until the operator has
// added the A record for the control-plane LB hostname — without
// it, kubeadm's TLS handshake against the configured endpoint fails
// at first-boot.
//
// Scope is intentionally JUST the control-plane hostname.
// keycloak.dns, netbird.dns, stun.dns, turn.dns all point at a
// SEPARATE ingress LB that doesn't exist yet — Traefik provisions
// it post-cluster-up. Lumping them in here meant the wait either
// hung (records pointing at an IP that hadn't been allocated) or
// passed against the wrong IP (operator pre-pointed everything at
// the control-plane LB to make the check pass, then had to fix it
// later anyway). Deferring those records to a post-Traefik check
// is the correct shape — left as follow-up work.
func waitForControlPlaneDNS(ctx context.Context, lbPublicIP string) error {
	return WaitForDNSResolution(ctx,
		[]string{globals.ControlPlaneHostname}, lbPublicIP)
}
