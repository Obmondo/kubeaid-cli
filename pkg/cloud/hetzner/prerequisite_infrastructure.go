// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

// ProvisionPrerequisiteInfrastructure provisions infrastructure required before CAPH starts
// spinning up the cluster.
//
//nolint:gocognit,nestif
func (h *Hetzner) ProvisionPrerequisiteInfrastructure(ctx context.Context) error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	if config.UsingHetznerBareMetal() {
		sshKeyPair := hetznerConfig.SSHKeyPair
		if err := h.CreateHetznerBareMetalSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig); err != nil {
			return fmt.Errorf("creating Hetzner Bare Metal SSH key: %w", err)
		}

		if err := h.InstallOSOnAllHBMS(ctx); err != nil {
			return fmt.Errorf("installing OS on HBMS: %w", err)
		}

		if err := h.GenerateStoragePlans(ctx, hetznerConfig); err != nil {
			return fmt.Errorf("generating storage plans: %w", err)
		}

		hydrateNodeGroupLabels(hetznerConfig)
	}

	if hetznerConfig.Mode == constants.HetznerModeBareMetal {
		return nil
	}

	bar := progress.FromCtx(ctx)

	network, err := h.CreateNetwork(ctx)
	if err != nil {
		return fmt.Errorf("creating Hetzner Network: %w", err)
	}
	bar.Substep("Created Hetzner Network")

	if config.UsingHCloud() {
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
		if err := h.CreateNATGateway(ctx, network.ID); err != nil {
			return fmt.Errorf("creating NAT gateway: %w", err)
		}
		bar.Substep("Created NAT Gateway")

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
	}

	if config.UsingHetznerBareMetal() {
		vswitchID, err := h.CreateVSwitch(ctx)
		if err != nil {
			return fmt.Errorf("creating VSwitch: %w", err)
		}

		if err := h.ConnectVSwitchWithHetznerNetwork(ctx, network); err != nil {
			return fmt.Errorf("connecting VSwitch with Hetzner Network: %w", err)
		}

		if config.ControlPlaneInHetznerBareMetal() {
			for _, host := range hetznerConfig.ControlPlane.BareMetal.BareMetalHosts {
				if err := h.AttachServerToVSwitch(ctx, host.ServerID, vswitchID); err != nil {
					return fmt.Errorf("attaching control-plane server %s to VSwitch: %w", host.ServerID, err)
				}
			}
		}

		for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
			for _, host := range nodeGroup.BareMetalHosts {
				if err := h.AttachServerToVSwitch(ctx, host.ServerID, vswitchID); err != nil {
					return fmt.Errorf("attaching node-group server %s to VSwitch: %w", host.ServerID, err)
				}
			}
		}
	}

	// Workload-cluster-connecting-to-VPN: attach the existing VPN
	// cluster's servers to this cluster's network so they share L2.
	// LB creation for this case happened above in preCreateControlPlaneLB.
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

// shouldPreCreateControlPlaneLB reports whether kubeaid-cli should
// provision the control-plane LB itself before CAPI runs. True for:
//   - cluster.type=vpn — this cluster IS the VPN; CoreDNS hosts
//     ConfigMap and the operator's DNS A-record both need the LB IPs
//     known at template-render time.
//   - HCloudVPNCluster set — workload connecting to an existing VPN;
//     CAPI HCloudCluster manifest references the pre-created LB by IP.
//
// False otherwise (workload-not-on-VPN), where CAPI handles LB
// lifecycle on its own.
func (h *Hetzner) shouldPreCreateControlPlaneLB() bool {
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
