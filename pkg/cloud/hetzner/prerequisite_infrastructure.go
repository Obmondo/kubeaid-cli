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

	// We won't be using Hetzner Network when cluster is purely on Hetzner Bare Metal.
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
		// Create the SSH key in Hetzner Bare Metal, if it doesn't already exist.
		sshKeyPair := hetznerConfig.SSHKeyPair
		h.CreateHetznerBareMetalSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig)

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

		// TODO : Boot each Hetzner Bare Metal server into rescue mode,
		//        so that KubeAid CLI can access them using the above Hetzner Bare Metal server.

		// Generate storage plan for the control-plane and each node-group.
		h.GenerateStoragePlans(ctx, hetznerConfig)

		// Apply node labels derived from the storage plan (e.g. disk=nvme).
		hydrateNodeGroupLabels(hetznerConfig)
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
