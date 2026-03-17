// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"k8c.io/kubeone/pkg/executor"
	kubeonessh "k8c.io/kubeone/pkg/ssh"
	"k8s.io/utils/ptr"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Returns IDs of the HCloud servers associated with the given Kubernetes cluster which was
// provisioned using Cluster API Provider Hetzner (CAPH).
func (h *Hetzner) GetHCloudServerIDsForCluster(ctx context.Context, name string) []int {
	server := h.hcloudClient.Server

	// Suppose the Kubernetes cluster name is vpn.
	// Since it was provisioned using Cluster API Provider Hetzner (CAPH), the associated HCloud
	// servers must have the "caph-cluster-vpn: owned" label attached.
	// So, we'll list those HCloud servers, which have the following label attached.
	servers, response, err := server.List(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: "caph-cluster-" + name,
		},
	})
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed listing HCloud servers associated with the given Kubernetes cluster",
		slog.String("cluster", name),
	)

	serverIDs := []int{}
	for _, server := range servers {
		serverIDs = append(serverIDs, server.ID)
	}
	return serverIDs
}

func (h *Hetzner) CreateNATGateway(ctx context.Context, networkID int) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner

	var (
		clusterName = config.ParsedGeneralConfig.Cluster.Name
		serverName  = fmt.Sprintf("%s-nat-gateway", clusterName)

		server *hcloud.Server
	)

	server, response, err := h.hcloudClient.Server.GetByName(ctx, serverName)
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed searching for the NAT gateway server",
		slog.String("server", serverName),
	)

	switch {
	case server != nil:
		slog.InfoContext(ctx, "NAT Gateway server already exists")

	// The HCloud server doesn't already exist. Let's create it.
	default:
		sshKeyPair, response, err := h.hcloudClient.SSHKey.GetByName(ctx, hetznerConfig.SSHKeyPair.Name)
		assert.Assert(ctx,
			((err == nil) && (response.StatusCode == http.StatusOK)),
			"Failed getting HCloud SSH keypair",
			logger.Error(err), slog.Any("response", response),
		)

		result, response, err := h.hcloudClient.Server.Create(ctx, hcloud.ServerCreateOpts{
			Name:       serverName,
			ServerType: &hcloud.ServerType{Name: constants.HCloudServerTypeCAX11},
			Image:      &hcloud.Image{Name: constants.HCloudServerImageUbuntu2404},
			SSHKeys:    []*hcloud.SSHKey{{ID: sshKeyPair.ID}},

			// Nuremberg and Falkenstein frequently run into unavailable HCloud servers issue.
			// So, we spin it up in Helsinki.
			Location: &hcloud.Location{Name: constants.HCloudLocationHel1},

			Networks: []*hcloud.Network{{ID: networkID}},
			PublicNet: &hcloud.ServerCreatePublicNet{
				EnableIPv4: true,
				EnableIPv6: false,
			},

			Labels: map[string]string{
				fmt.Sprintf("caph-cluster-%s", clusterName): "owned",
			},

			StartAfterCreate: ptr.To(true),
		})
		assert.Assert(ctx,
			((err == nil) && (response.StatusCode == http.StatusCreated)),
			"Failed creating NAT Gateway server",
			logger.Error(err), slog.Any("response", response),
		)
		slog.InfoContext(ctx, "Created NAT Gateway")

		server = result.Server
	}

	// Register the HCloud server as NAT Gateway for the Hetzner Network, if not already done.

	network, response, err := h.hcloudClient.Network.GetByID(ctx, networkID)
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed getting Hetzner Network",
		logger.Error(err), slog.Any("response", response),
	)

	var serverRegisteredAsNATGateway bool
	for _, route := range network.Routes {
		if route.Destination.String() == "0.0.0.0/0" {
			serverRegisteredAsNATGateway = true
			slog.InfoContext(ctx, "HCloud server already registered as NAT Gateway for the Hetzner Network")
		}
	}

	if !serverRegisteredAsNATGateway {
		_, response, err = h.hcloudClient.Network.AddRoute(ctx, &hcloud.Network{ID: networkID},
			hcloud.NetworkAddRouteOpts{
				Route: hcloud.NetworkRoute{
					Destination: &net.IPNet{
						IP:   net.IPv4(0, 0, 0, 0),
						Mask: net.CIDRMask(0, 32),
					},

					Gateway: server.PrivateNet[0].IP,
				},
			},
		)
		assert.Assert(ctx,
			((err == nil) && (response.StatusCode == http.StatusCreated)),
			"Failed registering HCloud server as NAT Gateway for the Hetzner Network",
			logger.Error(err), slog.Any("response", response),
		)
	}

	// Open an SSH connection to the NAT Gateway, and configure it.

	connector := kubeonessh.NewConnector(ctx)

	var connection executor.Interface
	for {
		var err error
		connection, err = kubeonessh.NewConnection(connector, kubeonessh.Opts{
			Context: ctx,

			Hostname:   server.PublicNet.IPv4.IP.String(),
			Port:       22,
			Username:   "root",
			PrivateKey: hetznerConfig.SSHKeyPair.PrivateKey,

			Timeout: time.Second * 10,
		})
		if err == nil {
			break
		}

		// An HCloud server isn't reachable just after creation.
		// So we'll wait for sometime, and retry.
		slog.InfoContext(ctx, "NAT Gateway server not reachable. Will retry after sometime....")
		time.Sleep(10 * time.Second)
	}
	defer connection.Close()

	_, _, _, err = connection.Exec(fmt.Sprintf(
		`
      echo 1 > /proc/sys/net/ipv4/ip_forward

      iptables -t nat -A POSTROUTING -s '%s' -o eth0 -j MASQUERADE
    `,
		hetznerConfig.HCloud.HetznerNetwork.CIDR,
	))
	assert.AssertErrNil(ctx, err, "Failed configuring NAT Gateway server")
	slog.InfoContext(ctx, "Configured NAT Gateway server")
}
