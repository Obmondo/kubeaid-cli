// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

var vSwitchID int

type ListVSwitchResponseBody = []struct {
	ID int `json:"id"`

	Name   string `json:"name"`
	VLANID int    `json:"vlan"`

	Cancelled bool `json:"cancelled"`
}

type CreateVSwitchResponseBody struct {
	ID int `json:"id"`

	Name   string `json:"name"`
	VLANID int    `json:"vlan"`

	Cancelled bool `json:"cancelled"`
}

// A VSwitch is used to establish private connectivity between Hetzner Bare Metal servers (and a
// Hetzner Network, when spinning up a Hetzner hybrid cluster).
// This function is responsible for creating that VSwitch, if it doesn't already exist.
// The VSwitch ID gets returned.
func (h *Hetzner) CreateVSwitch(ctx context.Context) (int, error) {
	vSwitchConfig := config.ParsedGeneralConfig.Cloud.Hetzner.BareMetal.VSwitch

	response, err := h.robotClient.NewRequest().Get("/vswitch")
	if err != nil {
		return 0, fmt.Errorf("listing VSwitches: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return 0, fmt.Errorf("listing VSwitches: unexpected status code %d", response.StatusCode())
	}

	listVSwitchResponseBody := ListVSwitchResponseBody{}

	err = json.Unmarshal(response.Body(), &listVSwitchResponseBody)
	if err != nil {
		return 0, fmt.Errorf("unmarshalling list VSwitch response body: %w", err)
	}

	for _, vSwitch := range listVSwitchResponseBody {
		if vSwitch.VLANID != vSwitchConfig.VLANID {
			continue
		}

		if vSwitch.Name != vSwitchConfig.Name {
			return 0, fmt.Errorf("a different VSwitch %q with the same VLANID exists; provide a different VLANID", vSwitch.Name)
		}

		if vSwitch.Cancelled {
			return 0, fmt.Errorf("vswitch exists but is being deleted (cancelled)")
		}

		vSwitchID = vSwitch.ID

		slog.InfoContext(ctx, "VSwitch already exists", slog.Int("id", vSwitchID))
		return vSwitchID, nil
	}

	response, err = h.robotClient.NewRequest().
		SetFormData(map[string]string{
			"name": vSwitchConfig.Name,
			"vlan": strconv.Itoa(vSwitchConfig.VLANID),
		}).
		Post("/vswitch")
	if err != nil {
		return 0, fmt.Errorf("creating VSwitch: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return 0, fmt.Errorf("creating VSwitch: unexpected status code %d", response.StatusCode())
	}

	createVSwitchResponseBody := new(CreateVSwitchResponseBody)
	err = json.Unmarshal(response.Body(), createVSwitchResponseBody)
	if err != nil {
		return 0, fmt.Errorf("unmarshalling create VSwitch response body: %w", err)
	}

	vSwitchID = createVSwitchResponseBody.ID

	slog.InfoContext(ctx, "Created VSwitch", slog.Int("id", vSwitchID))
	slog.WarnContext(ctx, "VSwitch deletion protection must be enabled manually in the Hetzner Robot console — Robot API does not support programmatic protection")

	return vSwitchID, nil
}

// When using Hetzner Bare Metal, we need to establish private connectivity between them and the
// HCloud servers in a Hetzner Network, using a VSwitch.
func (h *Hetzner) ConnectVSwitchWithHetznerNetwork(ctx context.Context, network *hcloud.Network) error {
	for _, subnet := range network.Subnets {
		if subnet.Type == hcloud.NetworkSubnetTypeVSwitch {
			if subnet.VSwitchID != vSwitchID {
				return fmt.Errorf("an unexpected VSwitch (id=%d) is already connected to the Hetzner Network", subnet.VSwitchID)
			}

			slog.InfoContext(ctx, "VSwitch is already connected to the Hetzner Network")
			return nil
		}
	}

	_, subnetCIDR, err := net.ParseCIDR(constants.HetznerVSwitchSubnetCIDR)
	if err != nil {
		return fmt.Errorf("parsing VSwitch Subnet CIDR: %w", err)
	}

	gatewayIP := net.ParseIP(constants.HetznerVSwitchGatewayIP)
	if gatewayIP == nil {
		return fmt.Errorf("parsing VSwitch Gateway IP %q", constants.HetznerVSwitchGatewayIP)
	}

	_, _, err = h.hcloudClient.Network.AddSubnet(ctx, network, hcloud.NetworkAddSubnetOpts{
		Subnet: hcloud.NetworkSubnet{
			Type:        hcloud.NetworkSubnetTypeVSwitch,
			IPRange:     subnetCIDR,
			NetworkZone: hcloud.NetworkZoneEUCentral,
			Gateway:     gatewayIP,
			VSwitchID:   vSwitchID,
		},
	})
	if err != nil {
		return fmt.Errorf("connecting VSwitch to Hetzner Network: %w", err)
	}

	slog.InfoContext(ctx, "Connected VSwitch to Hetzner Network")
	return nil
}

func (h *Hetzner) AttachServerToVSwitch(ctx context.Context, serverID string, vswitchID int) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-id", serverID), slog.Int("vswitch-id", vswitchID),
	})

	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"server": []string{serverID},
		}).
		Post(fmt.Sprintf("/vswitch/%d/server", vswitchID))
	if err != nil {
		return fmt.Errorf("attaching Hetzner Bare Metal server %s to VSwitch: %w", serverID, err)
	}

	switch response.StatusCode() {
	case http.StatusCreated, http.StatusConflict:
		slog.InfoContext(ctx, "Server is being attached to VSwitch")
		return nil

	default:
		return fmt.Errorf("attaching server %s to VSwitch: unexpected status code %d", serverID, response.StatusCode())
	}
}
