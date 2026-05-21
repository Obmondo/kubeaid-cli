// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/hetznercloud/hcloud-go/hcloud"
	"k8c.io/kubeone/pkg/executor"
	kubeonessh "k8c.io/kubeone/pkg/ssh"
	"k8s.io/utils/ptr"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
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
// named serverName if it exists, otherwise creates a fresh cax11
// (ARM) server attached to networkID with a public v4. ARM stock
// is uneven across HCloud datacenters and Hetzner returns
// resource_unavailable when the chosen DC is briefly out — try
// each location in constants.HCloudARMLocations in turn before
// giving up.
func (h *Hetzner) findOrCreateNATGatewayServer(ctx context.Context, serverName string, networkID int) (*hcloud.Server, error) {
	hetznerConfig := config.ParsedGeneralConfig.Cloud.Hetzner
	clusterName := config.ParsedGeneralConfig.Cluster.Name

	server, response, err := h.serverClient.GetByName(ctx, serverName)
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

	opts := hcloud.ServerCreateOpts{
		Name:       serverName,
		ServerType: &hcloud.ServerType{Name: constants.HCloudServerTypeCAX11},
		Image:      &hcloud.Image{Name: constants.HCloudServerImageUbuntu2404},
		SSHKeys:    []*hcloud.SSHKey{{ID: sshKeyPair.ID}},
		Networks:   []*hcloud.Network{{ID: networkID}},
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: true,
			EnableIPv6: false,
		},
		Labels: map[string]string{
			fmt.Sprintf("caph-cluster-%s", clusterName): "owned",
		},
		StartAfterCreate: ptr.To(true),
	}

	var lastErr error
	for _, location := range constants.HCloudARMLocations {
		opts.Location = &hcloud.Location{Name: location}

		result, response, err := h.serverClient.Create(ctx, opts)
		switch {
		case err == nil && response.StatusCode == http.StatusCreated:
			slog.InfoContext(ctx, "Created NAT Gateway server",
				slog.String("location", location))

			// Protect the NAT Gateway from accidental deletion AND
			// rebuild. Hetzner's API requires both flags to be sent
			// together (the action rejects requests that toggle only
			// one with `'delete' and 'rebuild' field required to be
			// the same value`).
			_, response, err = h.serverClient.ChangeProtection(ctx,
				result.Server,
				hcloud.ServerChangeProtectionOpts{
					Delete:  ptr.To(true),
					Rebuild: ptr.To(true),
				},
			)
			if err != nil {
				return nil, fmt.Errorf("enabling deletion protection on NAT Gateway server: %w", err)
			}
			// HCloud's ChangeProtection endpoint creates an Action and
			// returns 201 Created, not 200 OK. Accept both so we don't
			// flag the success case as an error.
			if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
				return nil, fmt.Errorf("enabling deletion protection on NAT Gateway server: unexpected status %d", response.StatusCode)
			}
			slog.InfoContext(ctx, "Enabled deletion protection on NAT Gateway server")

			return result.Server, nil

		case err != nil && isHCloudResourceUnavailable(err):
			slog.WarnContext(ctx, "NAT Gateway placement failed at location, trying next",
				slog.String("location", location),
				slog.String("error", err.Error()),
			)
			lastErr = err
			continue

		case err != nil:
			return nil, fmt.Errorf("creating NAT gateway server %q at %s: %w", serverName, location, err)

		default:
			return nil, fmt.Errorf("creating NAT gateway server %q at %s: unexpected status %d", serverName, location, response.StatusCode)
		}
	}

	return nil, fmt.Errorf("creating NAT gateway server %q: all %d ARM-capable HCloud locations returned resource_unavailable; last error: %w",
		serverName, len(constants.HCloudARMLocations), lastErr)
}

// isHCloudResourceUnavailable reports whether err is a Hetzner API
// error with code resource_unavailable — typically a transient
// out-of-stock condition for the requested server type at the
// chosen datacenter.
func isHCloudResourceUnavailable(err error) bool {
	var hcloudErr hcloud.Error
	if !errors.As(err, &hcloudErr) {
		return false
	}
	return hcloudErr.Code == hcloud.ErrorCodeResourceUnavailable
}

// ensureNATRouteOnNetwork registers a 0.0.0.0/0 route on the named
// Hetzner Network pointing at server's private IP, so other servers
// in the network egress through it. No-op when the route already
// exists.
func (h *Hetzner) ensureNATRouteOnNetwork(ctx context.Context, networkID int, server *hcloud.Server) error {
	network, response, err := h.networkClient.GetByID(ctx, networkID)
	if err != nil {
		return fmt.Errorf("getting Hetzner network %d: %w", networkID, err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("getting Hetzner network %d: unexpected status %d", networkID, response.StatusCode)
	}

	gatewayIP, err := h.ensureServerAttachedToNetwork(ctx, server, networkID)
	if err != nil {
		return err
	}

	for _, route := range network.Routes {
		if route.Destination.String() == "0.0.0.0/0" {
			slog.InfoContext(ctx, "HCloud server already registered as NAT Gateway for the Hetzner Network")
			return nil
		}
	}

	_, response, err = h.networkClient.AddRoute(ctx, &hcloud.Network{ID: networkID},
		hcloud.NetworkAddRouteOpts{
			Route: hcloud.NetworkRoute{
				Destination: &net.IPNet{
					IP:   net.IPv4(0, 0, 0, 0),
					Mask: net.CIDRMask(0, 32),
				},
				Gateway: gatewayIP,
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

// ensureServerAttachedToNetwork resolves the server's private IP in
// networkID. Hetzner's `Server.Create` with `Networks: [...]` returns
// before the attachment action completes, so the returned server's
// `PrivateNet` is often still empty for a few seconds. Same story on
// re-runs where `Server.GetByName`'s response races with an in-flight
// attach. We poll `GetByID` until the attachment shows up, then fall
// back to an explicit `AttachToNetwork` for the genuinely-orphaned
// case (manual console deletion that left a different server stale).
func (h *Hetzner) ensureServerAttachedToNetwork(ctx context.Context, server *hcloud.Server, networkID int) (net.IP, error) {
	if ip := privateIPOnNetwork(server, networkID); ip != nil {
		return ip, nil
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		fresh, _, err := h.serverClient.GetByID(ctx, server.ID)
		if err != nil {
			return nil, fmt.Errorf("re-fetching NAT gateway server %d: %w", server.ID, err)
		}
		if fresh == nil {
			return nil, fmt.Errorf("NAT gateway server %d not found on re-fetch", server.ID)
		}
		if ip := privateIPOnNetwork(fresh, networkID); ip != nil {
			return ip, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	// Hetzner's auto-attach (via Networks in CreateOpts) didn't fire
	// within the deadline — likely a stale orphan. Attempt an
	// explicit attach.
	slog.InfoContext(ctx, "NAT gateway server still not attached to network after polling; attaching explicitly",
		slog.Int64("server-id", int64(server.ID)),
		slog.Int("network-id", networkID),
	)
	_, _, err := h.serverClient.AttachToNetwork(ctx, server, hcloud.ServerAttachToNetworkOpts{
		Network: &hcloud.Network{ID: networkID},
	})
	if err != nil {
		return nil, fmt.Errorf("attaching NAT gateway server %d to network %d: %w", server.ID, networkID, err)
	}

	attached, _, err := h.serverClient.GetByID(ctx, server.ID)
	if err != nil {
		return nil, fmt.Errorf("re-fetching NAT gateway server %d after attach: %w", server.ID, err)
	}
	if ip := privateIPOnNetwork(attached, networkID); ip != nil {
		return ip, nil
	}
	return nil, fmt.Errorf("NAT gateway server %d still not attached to network %d after explicit attach", server.ID, networkID)
}

// privateIPOnNetwork returns the server's private IP within networkID,
// or nil when the server has no attachment to that network.
func privateIPOnNetwork(server *hcloud.Server, networkID int) net.IP {
	for _, pn := range server.PrivateNet {
		if pn.Network != nil && pn.Network.ID == networkID {
			return pn.IP
		}
	}
	return nil
}

// waitForNATGatewaySSH polls SSH on the freshly-created HCloud
// server until it accepts a connection. HCloud servers are not
// SSH-reachable for tens of seconds after Server.Create returns;
// retry every 10s instead of failing the bootstrap.
//
// Auth strategy:
//
//   - When SSH_AUTH_SOCK is set, the connection is routed through
//     the agent. This is the yubikey path — the private key
//     never leaves the hardware module; kubeone's ssh package
//     signs through the agent socket.
//   - Falls back to the operator-provided private key bytes when
//     no agent is present. Same as the pre-yubikey behaviour.
//
// Both can be set; kubeone tries the agent first and falls through
// to PrivateKey on agent failure (e.g. yubikey unplugged mid-run).
//
// The retry loop is unbounded by design — same behaviour as the
// original inline loop in CreateNATGateway. Cancellation flows
// through ctx (kubeonessh.NewConnection takes Context).
func waitForNATGatewaySSH(ctx context.Context, server *hcloud.Server, privateKey string) executor.Interface {
	connector := kubeonessh.NewConnector(ctx)

	opts := kubeonessh.Opts{
		Context:     ctx,
		Hostname:    server.PublicNet.IPv4.IP.String(),
		Port:        22,
		Username:    "root",
		AgentSocket: os.Getenv(constants.EnvNameSSHAuthSock),
		PrivateKey:  []byte(privateKey),
		Timeout:     time.Second * 10,
	}

	// Surface the YubiKey-touch hint while we're attempting the SSH
	// handshake — agent-routed signatures pause silently for the
	// touch, and the operator otherwise has no signal that the bar
	// is waiting on them. No-op when SSH_AUTH_SOCK is unset.
	releaseTouchHint := progress.FromCtx(ctx).RequestYubiKeyTouch("SSH into NAT gateway")
	defer releaseTouchHint()

	for {
		connection, err := kubeonessh.NewConnection(connector, opts)
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
