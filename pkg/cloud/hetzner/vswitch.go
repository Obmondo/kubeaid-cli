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

// GetVSwitchResponseBody is the subset of GET /vswitch/{id} we need
// to tell which servers are already attached.
type GetVSwitchResponseBody struct {
	ID int `json:"id"`

	Server []struct {
		ServerNumber int    `json:"server_number"`
		Status       string `json:"status"`
	} `json:"server"`
}

// AttachServersToVSwitch attaches every given bare-metal server to the
// vSwitch and blocks until all of them are actually attached (status
// "ready"), not merely enqueued.
//
// Hetzner applies vSwitch membership asynchronously: POST
// /vswitch/{id}/server accepts an array of server IDs, moves each to
// "in process", and rejects any further POST while busy with 409
// VSWITCH_IN_PROCESS. So we:
//
//   - GET the vSwitch and POST (in ONE request) every requested server
//     not already "ready"/"in process" — one atomic call, no per-server
//     race.
//   - Poll GET until every requested server reports "ready". "in
//     process" means keep waiting; "failed" (or a server that fell out)
//     gets re-POSTed on the next tick.
//
// Bounded by HBMSVSwitchAttachMaxWaitTime so a server Robot never
// brings to "ready" fails the bootstrap instead of hanging forever.
func (h *Hetzner) AttachServersToVSwitch(ctx context.Context, serverIDs []string, vswitchID int) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{slog.Int("vswitch-id", vswitchID)})

	if len(serverIDs) == 0 {
		return nil
	}

	deadline := time.Now().Add(constants.HBMSVSwitchAttachMaxWaitTime)

	for {
		statuses, err := h.vSwitchServerStatuses(vswitchID)
		if err != nil {
			return err
		}

		// Partition requested servers by current status.
		var notReady, toPost []string
		for _, serverID := range serverIDs {
			switch statuses[serverID] {
			case constants.HRobotVSwitchServerStatusReady:
				// Attached — nothing to do.
			case constants.HRobotVSwitchServerStatusInProcess:
				// Being attached — wait, don't re-POST.
				notReady = append(notReady, serverID)
			default:
				// "failed", or not on the vSwitch at all — (re)POST it.
				notReady = append(notReady, serverID)
				toPost = append(toPost, serverID)
			}
		}

		if len(notReady) == 0 {
			slog.InfoContext(ctx, "All servers are attached to VSwitch",
				slog.Any("server-ids", serverIDs),
			)
			return nil
		}

		if len(toPost) > 0 {
			if err := h.postServersToVSwitch(ctx, toPost, vswitchID); err != nil {
				return err
			}
		}

		if !time.Now().Before(deadline) {
			return fmt.Errorf("timed out waiting for servers %v to attach to VSwitch %d (max wait %v)", notReady, vswitchID, constants.HBMSVSwitchAttachMaxWaitTime)
		}

		slog.InfoContext(ctx, "Waiting for servers to finish attaching to VSwitch...",
			slog.Any("pending-server-ids", notReady),
			slog.Duration("interval", constants.HBMSVSwitchAttachPollInterval),
		)
		h.sleepFunc(constants.HBMSVSwitchAttachPollInterval)
	}
}

// postServersToVSwitch issues the batch POST that enqueues the given
// servers for attachment. A 409 VSWITCH_IN_PROCESS is not an error —
// the vSwitch is busy applying a prior update; the caller's next poll
// tick retries. Any other non-201 status is a hard failure.
func (h *Hetzner) postServersToVSwitch(ctx context.Context, serverIDs []string, vswitchID int) error {
	response, err := h.robotClient.NewRequest().
		SetFormDataFromValues(url.Values{
			"server[]": serverIDs,
		}).
		Post(fmt.Sprintf("/vswitch/%d/server", vswitchID))
	if err != nil {
		return fmt.Errorf("attaching Hetzner Bare Metal servers %v to VSwitch: %w", serverIDs, err)
	}

	switch {
	case response.StatusCode() == http.StatusCreated:
		slog.InfoContext(ctx, "Requested servers to be attached to VSwitch",
			slog.Any("server-ids", serverIDs),
		)
		return nil

	case response.StatusCode() == http.StatusConflict &&
		hRobotErrorCode(response.Body()) == constants.HRobotVSwitchInProcessErrorCode:
		// vSwitch busy; the poll loop will retry on the next tick.
		return nil

	default:
		return fmt.Errorf("attaching servers %v to VSwitch: unexpected status code %d", serverIDs, response.StatusCode())
	}
}

// vSwitchServerStatuses returns a map of server ID → vSwitch
// attachment status ("ready"/"in process"/"failed") for every server
// currently on the vSwitch. Servers absent from the map aren't on the
// vSwitch at all.
func (h *Hetzner) vSwitchServerStatuses(vswitchID int) (map[string]string, error) {
	response, err := h.robotClient.NewRequest().Get(fmt.Sprintf("/vswitch/%d", vswitchID))
	if err != nil {
		return nil, fmt.Errorf("getting VSwitch %d: %w", vswitchID, err)
	}
	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("getting VSwitch %d: unexpected status code %d", vswitchID, response.StatusCode())
	}

	getVSwitchResponseBody := new(GetVSwitchResponseBody)
	if err := json.Unmarshal(response.Body(), getVSwitchResponseBody); err != nil {
		return nil, fmt.Errorf("unmarshalling get VSwitch response body: %w", err)
	}

	statuses := make(map[string]string, len(getVSwitchResponseBody.Server))
	for _, server := range getVSwitchResponseBody.Server {
		statuses[strconv.Itoa(server.ServerNumber)] = server.Status
	}
	return statuses, nil
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
