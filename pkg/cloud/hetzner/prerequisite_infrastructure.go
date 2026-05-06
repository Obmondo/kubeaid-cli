// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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

	network, err := h.CreateNetwork(ctx)
	if err != nil {
		return fmt.Errorf("creating Hetzner Network: %w", err)
	}

	if config.UsingHCloud() {
		sshKeyPair := hetznerConfig.SSHKeyPair
		if err := h.CreateHCloudSSHKey(ctx, sshKeyPair.Name, sshKeyPair.SSHKeyPairConfig); err != nil {
			return fmt.Errorf("creating HCloud SSH key: %w", err)
		}

		if err := h.CreateNATGateway(ctx, network.ID); err != nil {
			return fmt.Errorf("creating NAT gateway: %w", err)
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

		controlPlaneHostname := hetznerConfig.ControlPlane.HCloud.LoadBalancer.Endpoint
		loadBalancer, err := h.CreateLB(ctx,
			config.ParsedGeneralConfig.Cluster.Name,
			network,
			config.ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud.LoadBalancer.Region,
			controlPlaneHostname != "",
		)
		if err != nil {
			return fmt.Errorf("creating control-plane LB: %w", err)
		}

		globals.ControlPlaneLBPrivateIP = loadBalancer.PrivateNet[0].IP.String()
		globals.ControlPlaneHostname = controlPlaneHostname

		if controlPlaneHostname != "" {
			assert.Assert(ctx,
				loadBalancer.PublicNet.Enabled && loadBalancer.PublicNet.IPv4.IP != nil,
				"Control-plane LB hostname requires a bootstrap public LB IP, but the LB has no public IPv4",
				slog.String("hostname", controlPlaneHostname),
			)

			globals.ControlPlaneLBBootstrapPublicIP = loadBalancer.PublicNet.IPv4.IP.String()
		}
	}

	return nil
}
