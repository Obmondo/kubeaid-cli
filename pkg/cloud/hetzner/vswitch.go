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
	"os"
	"strconv"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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
func (h *Hetzner) CreateVSwitch(ctx context.Context) int {
	vSwitchConfig := config.ParsedGeneralConfig.Cloud.Hetzner.VSwitch

	// List all the VSwitches and see whether a VSwitch with the given name and VLAN ID already
	// exists.

	response, err := h.robotClient.NewRequest().Get("/vswitch")

	assert.AssertErrNil(ctx, err, "Failed listing VSwitches")
	assert.Assert(ctx,
		(response.StatusCode() == http.StatusOK),
		"Failed listing VSwitches",
		slog.Any("response", response),
	)

	listVSwitchResponseBody := ListVSwitchResponseBody{}

	err = json.Unmarshal(response.Body(), &listVSwitchResponseBody)
	assert.AssertErrNil(ctx, err, "Failed JSON unmarshalling list VSwitch response body")

	for _, vSwitch := range listVSwitchResponseBody {
		if ((vSwitch.Name == vSwitchConfig.Name) && (vSwitch.VLANID == vSwitchConfig.VLANID)) &&
			!vSwitch.Cancelled {
			// VSwitch already exists.
			// So, we don't need to do anything else.

			vSwitchID = vSwitch.ID

			slog.InfoContext(ctx, "VSwitch already exists", slog.Int("id", vSwitchID))
			return vSwitchID
		}
	}

	// The VSwitch doesn't already exist.
	// So, let's create it.

	response, err = h.robotClient.NewRequest().
		SetFormData(map[string]string{
			"name": vSwitchConfig.Name,
			"vlan": strconv.Itoa(vSwitchConfig.VLANID),
		}).
		Post("/vswitch")

	assert.AssertErrNil(ctx, err, "Failed creating VSwitch")
	assert.Assert(ctx,
		(response.StatusCode() == http.StatusOK),
		"Failed creating VSwitch",
		slog.Any("response", response),
	)

	createVSwitchResponseBody := new(CreateVSwitchResponseBody)
	err = json.Unmarshal(response.Body(), createVSwitchResponseBody)
	assert.AssertErrNil(ctx, err, "Failed JSON unmarshalling create VSwitch response body")

	vSwitchID = createVSwitchResponseBody.ID

	slog.InfoContext(ctx, "Created VSwitch", slog.Int("id", vSwitchID))

	return vSwitchID
}

// When using Hetzner Bare Metal, we need to establish private connectivity between them and the
// HCloud servers in a Hetzner Network, using a VSwitch.
func (h *Hetzner) ConnectVSwitchWithHetznerNetwork(ctx context.Context, network *hcloud.Network) {
	// Check whether the VSwitch is already connected to the Hetzner Network.
	for _, subnet := range network.Subnets {
		if subnet.Type == hcloud.NetworkSubnetTypeVSwitch {
			// Only 1 VSwitch can be connected with an Hetzner Network.
			// And it must be the VSwitch we're expecting.
			assert.Assert(ctx,
				(subnet.VSwitchID == vSwitchID),
				"An unexpected VSwitch is already connected to the Hetzner Network",
				slog.Int("vswitch-id", subnet.VSwitchID),
			)

			slog.InfoContext(ctx, "VSwitch is already connected to the Hetzner Network")
			return
		}
	}

	// If not, then establish private connectivity by creating a VSwitch Subnet.

	_, subnetCIDR, err := net.ParseCIDR(constants.HetznerVSwitchSubnetCIDR)
	assert.AssertErrNil(ctx, err, "Failed parsing VSwitch Subnet CIDR")

	gatewayIP := net.ParseIP(constants.HetznerVSwitchGatewayIP)
	assert.AssertNotNil(ctx, gatewayIP, "Failed parsing VSwitch Gateway IP")

	_, _, err = h.hcloudClient.Network.AddSubnet(ctx, network, hcloud.NetworkAddSubnetOpts{
		Subnet: hcloud.NetworkSubnet{
			Type:        hcloud.NetworkSubnetTypeVSwitch,
			IPRange:     subnetCIDR,
			NetworkZone: hcloud.NetworkZoneEUCentral,
			Gateway:     gatewayIP,
			VSwitchID:   vSwitchID,
		},
	})
	assert.AssertErrNil(ctx, err, "Failed connecting VSwitch to Hetzner Network")

	slog.InfoContext(ctx, "Connected VSwitch to Hetzner Network")
}

func (h *Hetzner) AttachServerToVSwitch(ctx context.Context, serverID string, vswitchID int) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-id", serverID), slog.Int("vswitch-id", vSwitchID),
	})

	// BUG : Even if I specify multiple serversIDs, only 1 server gets attached to the VSwitch.
	//       So, as of now, I am sending 1 request per server.
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"server": []string{serverID},
		}).
		Post(fmt.Sprintf("/vswitch/%d/server", vSwitchID))

	assert.AssertErrNil(ctx, err, "Failed attaching Hetzner Bare Metal server to VSwitch")

	switch response.StatusCode() {
	case http.StatusCreated, http.StatusConflict:
		slog.InfoContext(ctx, "Server is being attached to VSwitch")

	default:
		slog.ErrorContext(ctx, "Received unexpected response statuscode", slog.Any("response", response))
		os.Exit(1)
	}
}
