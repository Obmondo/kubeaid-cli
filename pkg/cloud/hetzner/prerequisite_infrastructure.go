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

	/*
		We won't be using Hetzner Network when cluster is purely on Hetzner Bare Metal.

		TODO : Make use of Hetzner Network when we have a VPN cluster.

		       When the control-plane is in Hetzner Bare Metal, then we don't need any HCloud
		       LoadBalancer / Failover IP. We'll create a DNS entry in each node, pointing to the
		       control-plane nodes' private IPs. That DNS entry name will be the control-plane
		       endpoint.
	*/
	if hetznerConfig.Mode == constants.HetznerModeBareMetal {
		return
	}

	// Ensure that the Hetzner Network is created.
	network := h.CreateNetwork(ctx)

	if config.UsingHCloud() {
		// TODO : Create the HCloud SSH KeyPair, if it doesn't already exist.

		// Ensure that a NAT Gateway exists.
		h.CreateNATGateway(ctx, network.ID)
	}

	if config.UsingHetznerBareMetal() {
		// TODO : Create the Hetzner Bare Metal SSH KeyPair, if it doesn't already exist.

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

		switch {
		// When the control-plane is in HCloud,
		case config.ControlPlaneInHCloud():
			// Ensure that the LoadBalancer corresponding to the Kubernetes API server is created and
			// attached to the Hetzner Network. The private IP of that LoadBalancer will be specified as
			// the control-plane endpoint to CAPI.

			loadBalancer := h.CreateLB(ctx,
				config.ParsedGeneralConfig.Cluster.Name,
				network,
				config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud.LoadBalancer.Region,
			)

			globals.PreProvisionedControlPlaneLBIP = loadBalancer.PrivateNet[0].IP.String()

		case config.ControlPlaneInHetznerBareMetal():
			// TODO : We'll create a DNS entry in each node, pointing to the control-plane nodes' private
			//        IPs. That DNS entry name will be the control-plane endpoint.
			break
		}

		/*
			TODO : Ensure that this machine, from where KubeAid CLI is getting executed, is connected
			       to the Hetzner network using NetBird. We can do this by :

			        (1) picking up one of the VPN control-plane nodes. And retrieving it's private IP
			            in this Hetzner Network.

			        (2) pinging that VPN control-plane node using that private IP, ensuring that the ping
			            succeeds.
		*/
	}
}
