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
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
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

	vSwitchSubnetCIDR := config.ParsedGeneralConfig.Cloud.Hetzner.BareMetal.VSwitch.SubnetCIDRBlock

	gatewayIP, subnetCIDR, err := net.ParseCIDR(vSwitchSubnetCIDR)
	if err != nil {
		return fmt.Errorf("parsing VSwitch Subnet CIDR %q: %w", vSwitchSubnetCIDR, err)
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

// AttachServerToVSwitch registers a bare-metal server against the
// vSwitch. Hetzner applies the change asynchronously: the first
// server's POST returns 201 and the vSwitch then enters an "in
// process" state, so an attach of a second server issued while that
// update is still running is rejected with 409 VSWITCH_IN_PROCESS.
// That is a transient "busy", NOT "already attached" — the server is
// silently dropped if we don't retry, which is why only the first
// server used to land. We poll-retry on that code until the prior
// update settles and this attach takes. Any other 409 (VLAN clash,
// server limit) and every other non-201 status is a hard failure.
func (h *Hetzner) AttachServerToVSwitch(ctx context.Context, serverID string, vswitchID int) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("server-id", serverID), slog.Int("vswitch-id", vswitchID),
	})

	deadline := time.Now().Add(constants.HBMSVSwitchAttachMaxWaitTime)

	for {
		response, err := h.robotClient.NewRequest().
			SetFormDataFromValues(url.Values{
				"server": []string{serverID},
			}).
			Post(fmt.Sprintf("/vswitch/%d/server", vswitchID))
		if err != nil {
			return fmt.Errorf("attaching Hetzner Bare Metal server %s to VSwitch: %w", serverID, err)
		}

		if response.StatusCode() == http.StatusCreated {
			slog.InfoContext(ctx, "Server is being attached to VSwitch")
			return nil
		}

		// The vSwitch is still applying a previous attach; wait for it
		// to settle, then retry this server.
		if response.StatusCode() == http.StatusConflict &&
			hRobotErrorCode(response.Body()) == constants.HRobotVSwitchInProcessErrorCode {
			if !time.Now().Before(deadline) {
				return fmt.Errorf("timed out waiting for VSwitch to become available to attach server %s (max wait %v)", serverID, constants.HBMSVSwitchAttachMaxWaitTime)
			}

			slog.InfoContext(ctx, "VSwitch is busy applying a previous update, will retry attaching server...",
				slog.Duration("interval", constants.HBMSVSwitchAttachPollInterval),
			)
			h.sleepFunc(constants.HBMSVSwitchAttachPollInterval)
			continue
		}

		return fmt.Errorf("attaching server %s to VSwitch: unexpected status code %d", serverID, response.StatusCode())
	}
}

// hRobotErrorResponseBody is Hetzner Robot's standard error envelope.
type hRobotErrorResponseBody struct {
	Error struct {
		Status  int    `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// hRobotErrorCode extracts the Robot error code from a response body,
// returning "" when the body isn't the standard error envelope.
func hRobotErrorCode(body []byte) string {
	errorResponseBody := new(hRobotErrorResponseBody)
	if err := json.Unmarshal(body, errorResponseBody); err != nil {
		return ""
	}
	return errorResponseBody.Error.Code
}
