// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
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

// A VSwitch is used to establish private connectivity between Hetzner Bare Metal servers (and an
// HCloud Network, when spinning up a Hetzner hybrid cluster).
// This function is responsible for creating that VSwitch, if it doesn't already exist.
func (h *Hetzner) CreateVSwitch(ctx context.Context) {
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
			return
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
}

// When using Hetzner Bare Metal, we need to establish private connectivity between the Hetzner
// Network in HCloud and the Hetzner Bare Metal servers.
// As of now, CAPH doesn't allow us to use a pre-created HCloud Network. So, we need to sync the
// capi-cluster ArgoCD App, and wait for CAPH to create the HCloud Network. We can then pick up
// its ID from the HetznerCluster resource.
func (h *Hetzner) ConnectVSwitchWithHCloudNetwork(ctx context.Context) {
	// Construct cluster client.
	kubeconfig := utils.MustGetEnv(constants.EnvNameKubeconfig)
	clusterClient := kubernetes.MustCreateClusterClient(ctx, kubeconfig)

	// Wait for CAPH to create the HCloud Network.
	// Then, get it's ID from the HetznerCluster resource.

	var networkID int

	_ = wait.PollUntilContextCancel(ctx, 5*time.Second, false,
		func(ctx context.Context) (bool, error) {
			// Get the HetznerCluster resource.
			hetznerCluster := &caphV1Beta1.HetznerCluster{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      config.ParsedGeneralConfig.Cluster.Name,
					Namespace: kubernetes.GetCapiClusterNamespace(),
				},
			}
			err := kubernetes.GetKubernetesResource(ctx, clusterClient, hetznerCluster)
			assert.AssertErrNil(ctx, err, "Failed getting the HetznerCluster resource")

			networkDetails := hetznerCluster.Status.Network
			if networkDetails == nil {
				return false, nil
			}

			networkID = int(networkDetails.ID)

			return true, nil
		},
	)
	slog.InfoContext(ctx, "Retrieved HCloud Network ID", slog.Int("id", networkID))

	// Get the HCloud Network.
	network, _, err := h.hcloudClient.Network.GetByID(ctx, networkID)
	assert.AssertErrNil(ctx, err, "Failed getting HCloud Network ID")

	// Check whether the VSwitch is already connected to the HCloud Network.
	for _, subnet := range network.Subnets {
		if subnet.Type == hcloud.NetworkSubnetTypeVSwitch {
			// Only 1 VSwitch can be connected with an HCloud Network.
			// And it must be the VSwitch we're expecting.
			assert.Assert(ctx,
				(subnet.VSwitchID == vSwitchID),
				"An unexpected VSwitch is already connected to the HCloud Network",
				slog.Int("vswitch-id", subnet.VSwitchID),
			)

			slog.InfoContext(ctx, "VSwitch is already connected to the HCloud Network")
			return
		}
	}

	// Establish private connectivity by creating a Subnet.

	_, subnetCIDR, err := net.ParseCIDR(constants.HetznerVSwitchSubnetCIDR)
	assert.AssertErrNil(ctx, err, "Failed parsing VSwitch Subnet CIDR")

	gatewayIP := net.ParseIP(constants.HetznerVSwitchGatewayIP)
	assert.AssertNotNil(ctx, gatewayIP, "Failed parsing VSwitch Gateway IP")

	_, _, err = h.hcloudClient.Network.AddSubnet(ctx, network, hcloud.NetworkAddSubnetOpts{
		Subnet: hcloud.NetworkSubnet{
			Type:      hcloud.NetworkSubnetTypeVSwitch,
			IPRange:   subnetCIDR,
			Gateway:   gatewayIP,
			VSwitchID: vSwitchID,
		},
	})
	assert.AssertErrNil(ctx, err, "Failed connecting VSwitch to HCloud Network")

	slog.InfoContext(ctx, "Connected VSwitch to HCloud Network")
}
