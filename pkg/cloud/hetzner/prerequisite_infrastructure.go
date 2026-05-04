// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

// Provisions infrastructure required before CAPH starts spinning up the cluster.
func (h *Hetzner) ProvisionPrerequisiteInfrastructure(ctx context.Context) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	// HBMS-specific steps (SSH key registration with HRobot, OS install, storage plans) must
	// run for any mode that includes bare-metal hosts: pure "bare-metal" and "hybrid". They
	// don't depend on Hetzner Network / VSwitch.
	if config.UsingHetznerBareMetal() {
		sshKeyPair := hetznerConfig.SSHKeyPair
		h.CreateHetznerBareMetalSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig)

		// Install the OS on each HBMS (if not already installed).
		// This activates a Linux installation via the HRobot API, triggers a hardware
		// reset, and waits until the HBMS is reachable via SSH.
		h.InstallOSOnAllHBMS(ctx)

		// Generate storage plan for the control-plane and each node-group.
		h.GenerateStoragePlans(ctx, hetznerConfig)

		// Apply node labels derived from the storage plan
		hydrateNodeGroupLabels(hetznerConfig)
	}

	// Hetzner Network / VSwitch are only needed when HCloud is in the picture (mode = hcloud
	// or hybrid). In pure bare-metal mode there is no Hetzner Network
	if hetznerConfig.Mode == constants.HetznerModeBareMetal {
		return
	}

	// From here, mode == "hcloud" || mode == "hybrid".
	// And, in both the cases, the control-plane will be in HCloud.

	// Ensure that the Hetzner Network is created.
	network := h.CreateNetwork(ctx)

	if config.UsingHCloud() {
		// Create the SSH key in HCloud, if it doesn't already exist.
		sshKeyPair := hetznerConfig.SSHKeyPair
		h.CreateHCloudSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig)

		// Ensure that a NAT Gateway exists.
		h.CreateNATGateway(ctx, network.ID)
	}

	if config.UsingHetznerBareMetal() {
		// Ensure that the required VSwitch is created.
		vswitchID := h.CreateVSwitch(ctx)

		// Ensure that the VSwitch is connected to that Hetzner Network.
		h.ConnectVSwitchWithHetznerNetwork(ctx, network)

		// Ensure that the Hetzner Bare Metal servers are attached to the VSwitch.

		if config.ControlPlaneInHetznerBareMetal() {
			for _, host := range hetznerConfig.ControlPlane.BareMetal.BareMetalHosts {
				h.AttachServerToVSwitch(ctx, host.ServerID, vswitchID)
			}
		}

		for _, nodeGroup := range hetznerConfig.NodeGroups.BareMetal {
			for _, host := range nodeGroup.BareMetalHosts {
				h.AttachServerToVSwitch(ctx, host.ServerID, vswitchID)
			}
		}
	}

	if hetznerConfig.HCloudVPNCluster != nil {
		// Ensure that the HCloud servers corresponding to the VPN cluster are attached to that
		// Hetzner Network.
		serverIDs := h.GetHCloudServerIDsForCluster(ctx,
			hetznerConfig.HCloudVPNCluster.Name,
		)
		for _, serverID := range serverIDs {
			h.AttachHCloudServerToNetwork(ctx, serverID, network.ID)
		}

		// Ensure that the LoadBalancer corresponding to the Kubernetes API server is created and
		// attached to the Hetzner Network. The private IP of that LoadBalancer will be specified as
		// the control-plane endpoint to CAPI.

		loadBalancer := h.CreateLB(ctx,
			config.ParsedGeneralConfig.Cluster.Name,
			network,
			config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud.LoadBalancer.Region,
		)

		globals.PreProvisionedControlPlaneLBIP = loadBalancer.PrivateNet[0].IP.String()
	}
}
