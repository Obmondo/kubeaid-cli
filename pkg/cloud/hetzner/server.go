// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
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
)

// GetHCloudServerIDsForCluster returns IDs of the HCloud servers associated with the given
// Kubernetes cluster which was provisioned using Cluster API Provider Hetzner (CAPH).
func (h *Hetzner) GetHCloudServerIDsForCluster(ctx context.Context, name string) ([]int, error) {
	servers, response, err := h.serverClient.List(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: "caph-cluster-" + name,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing HCloud servers for cluster %q: %w", name, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing HCloud servers for cluster %q: unexpected status %d", name, response.StatusCode)
	}

	serverIDs := make([]int, 0, len(servers))
	for _, s := range servers {
		serverIDs = append(serverIDs, s.ID)
	}
	return serverIDs, nil
}

func (h *Hetzner) CreateNATGateway(ctx context.Context, networkID int) error {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	clusterName := config.ParsedGeneralConfig.Cluster.Name
	serverName := fmt.Sprintf("%s-nat-gateway", clusterName)

	server, err := h.findOrCreateNATGatewayServer(ctx, serverName, networkID)
	if err != nil {
		return err
	}

	if err := h.ensureNATRouteOnNetwork(ctx, networkID, server); err != nil {
		return err
	}

	connection := waitForNATGatewaySSH(ctx, server, hetznerConfig.SSHKeyPair.PrivateKey)
	defer connection.Close()

	cidr := hetznerConfig.HCloud.HetznerNetwork.CIDR
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	_, _, _, err = connection.Exec(fmt.Sprintf(
		`
      echo 'net.ipv4.ip_forward=1' > /etc/sysctl.d/99-nat-gateway.conf
      sysctl -p /etc/sysctl.d/99-nat-gateway.conf

      export DEBIAN_FRONTEND=noninteractive
      apt-get update
      apt-get install -y iptables-persistent

      # Egress interface = whichever interface the kernel picks to
      # reach the public internet. Probed via Hetzner's own
      # recursive resolver IP (185.12.64.1) — robust against later
      # NetBird routes since NetBird only handles mesh peers, not
      # arbitrary public IPs.
      egress_interface=$(ip route get 185.12.64.1 | awk '{print $5; exit}')
      iptables -t nat -C POSTROUTING -s %[1]s -o "$egress_interface" -j MASQUERADE 2>/dev/null || \
      iptables -t nat -A POSTROUTING -s %[1]s -o "$egress_interface" -j MASQUERADE

      mkdir -p /etc/iptables
      iptables-save > /etc/iptables/rules.v4

      systemctl enable --now netfilter-persistent
    `,
		cidr,
	))
	if err != nil {
		return fmt.Errorf("configuring NAT gateway server: %w", err)
	}
	slog.InfoContext(ctx, "Configured NAT Gateway server")
	return nil
}

// findOrCreateNATGatewayServer returns the existing HCloud server
// named serverName if it exists, otherwise creates one (cax11 in
// hel1, attached to networkID, with a public v4) and returns the
// fresh server. Extracted from CreateNATGateway to keep that
// function's cognitive complexity in check.
func (h *Hetzner) findOrCreateNATGatewayServer(ctx context.Context, serverName string, networkID int) (*hcloud.Server, error) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	server, response, err := h.hcloudClient.Server.GetByName(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("searching for NAT gateway server %q: %w", serverName, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searching for NAT gateway server %q: unexpected status %d", serverName, response.StatusCode)
	}
	if server != nil {
		slog.InfoContext(ctx, "NAT Gateway server already exists")
		return server, nil
	}

	sshKeyPair, response, err := h.hcloudClient.SSHKey.GetByName(ctx, hetznerConfig.SSHKeyPair.Name)
	if err != nil {
		return nil, fmt.Errorf("getting HCloud SSH keypair %q: %w", hetznerConfig.SSHKeyPair.Name, err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getting HCloud SSH keypair %q: unexpected status %d", hetznerConfig.SSHKeyPair.Name, response.StatusCode)
	}

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
	if err != nil {
		return nil, fmt.Errorf("creating NAT gateway server %q: %w", serverName, err)
	}
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("creating NAT gateway server %q: unexpected status %d", serverName, response.StatusCode)
	}
	slog.InfoContext(ctx, "Created NAT Gateway server")
	return result.Server, nil
}

// ensureNATRouteOnNetwork registers a 0.0.0.0/0 route on the named
// Hetzner Network pointing at server's private IP, so other servers
// in the network egress through it. No-op when the route already
// exists.
func (h *Hetzner) ensureNATRouteOnNetwork(ctx context.Context, networkID int, server *hcloud.Server) error {
	network, response, err := h.hcloudClient.Network.GetByID(ctx, networkID)
	if err != nil {
		return fmt.Errorf("getting Hetzner network %d: %w", networkID, err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("getting Hetzner network %d: unexpected status %d", networkID, response.StatusCode)
	}

	for _, route := range network.Routes {
		if route.Destination.String() == "0.0.0.0/0" {
			slog.InfoContext(ctx, "HCloud server already registered as NAT Gateway for the Hetzner Network")
			return nil
		}
	}

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
	if err != nil {
		return fmt.Errorf("registering NAT gateway route on network %d: %w", networkID, err)
	}
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("registering NAT gateway route on network %d: unexpected status %d", networkID, response.StatusCode)
	}
	return nil
}

// waitForNATGatewaySSH polls SSH on the freshly-created HCloud
// server until it accepts a connection. HCloud servers are not
// SSH-reachable for tens of seconds after Server.Create returns;
// retry every 10s instead of failing the bootstrap.
//
// The retry loop is unbounded by design — same behaviour as the
// original inline loop in CreateNATGateway. Cancellation flows
// through ctx (kubeonessh.NewConnection takes Context). Returns
// only the connection because the loop never gives up under its
// own steam.
func waitForNATGatewaySSH(ctx context.Context, server *hcloud.Server, privateKey string) executor.Interface {
	connector := kubeonessh.NewConnector(ctx)

	for {
		connection, err := kubeonessh.NewConnection(connector, kubeonessh.Opts{
			Context:    ctx,
			Hostname:   server.PublicNet.IPv4.IP.String(),
			Port:       22,
			Username:   "root",
			PrivateKey: []byte(privateKey),
			Timeout:    time.Second * 10,
		})
		if err == nil {
			return connection
		}

		slog.InfoContext(ctx, "NAT Gateway server not reachable. Will retry after sometime....")
		time.Sleep(10 * time.Second)
	}
}

type (
	GetServerResponseBody struct {
		Server Server `json:"server"`
	}

	Server struct {
		IP string `json:"server_ip"`
	}
)

// getHetznerBareMetalServerIP fetches the public IPv4 address of the Hetzner bare-metal
// server with the given ID.
func (h *Hetzner) getHetznerBareMetalServerIP(id string) (string, error) {
	response, err := h.robotClient.R().Get("/server/" + id)
	if err != nil {
		return "", fmt.Errorf("getting server %s details: %w", id, err)
	}
	if response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("getting server %s details: unexpected status %d", id, response.StatusCode())
	}

	var body GetServerResponseBody
	if err := json.Unmarshal(response.Body(), &body); err != nil {
		return "", fmt.Errorf("unmarshalling server %s response: %w", id, err)
	}

	return body.Server.IP, nil
}
