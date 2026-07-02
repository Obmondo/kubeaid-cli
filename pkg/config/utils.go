// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"context"
	"encoding/json"
	"net/http"
	"path"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

type ReleaseDetails struct {
	TagName string `json:"tag_name"`
}

// Returns the latest KubeAid version, fetching it from GitHub.
//
//nolint:unused
func getLatestKubeAidVersion(ctx context.Context) string {
	response, err := http.DefaultClient.Get(
		"https://api.github.com/repos/Obmondo/KubeAid/releases/latest",
	)
	assert.AssertErrNil(ctx, err, "Failed getting KubeAid's latest release details")
	defer response.Body.Close()

	assert.Assert(ctx,
		(response.StatusCode == http.StatusOK),
		"Failed getting KubeAid's latest release details",
	)

	var releaseDetails ReleaseDetails
	err = json.NewDecoder(response.Body).Decode(&releaseDetails)
	assert.AssertErrNil(ctx, err, "Failed JSON decoding KubeAid's latest release details")

	return releaseDetails.TagName
}

func GetGeneralConfigFilePath() string {
	return path.Join(globals.ConfigsDirectory, "general.yaml")
}

func GetSecretsConfigFilePath() string {
	return path.Join(globals.ConfigsDirectory, "secrets.yaml")
}

// Returns whether we're using HCloud.
func UsingHCloud() bool {
	if ParsedGeneralConfig.Cloud.Hetzner == nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid)
}

// Returns whether the control-plane is in HCloud.
func ControlPlaneInHCloud() bool {
	if ParsedGeneralConfig.Cloud.Hetzner == nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid)
}

// CoturnFloatingIPEnabled reports whether kubeaid-cli should provision an
// HCloud Floating IP for NetBird Coturn (STUN/TURN) HA — and, with it,
// the hcloud-fip-controller app, the Coturn DaemonSet overlay, and the
// per-CP netplan binding. True only for a multi-control-plane HCloud VPN
// cluster: a VPN cluster runs Coturn, and with more than one CP the
// active Coturn can land on any node, so its public IP must float. A
// single-CP VPN cluster has no failover (Coturn stays on its one node),
// and a non-VPN cluster runs no Coturn at all.
func CoturnFloatingIPEnabled() bool {
	if ParsedGeneralConfig.Cluster.Type != constants.ClusterTypeVPN {
		return false
	}
	if !UsingHCloud() {
		return false
	}
	if ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud == nil {
		return false
	}
	return ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud.Replicas > 1
}

// HCloudSingleNodePublicVPN reports whether this is the single-node
// public-control-plane VPN topology: an HCloud VPN cluster with exactly one
// control-plane replica and no HCloud worker node-groups. In that case
// kubeaid-cli skips the NAT gateway + control-plane LB and puts the CP node on
// a primary public IPv4 that api / stun / turn resolve to (netbird / keycloak
// still go through the Traefik ingress LB). Derived, not opted into — the
// counterpart of CoturnFloatingIPEnabled (which keys off Replicas > 1). A
// private worker node-group would have no NAT gateway to egress through, so the
// topology only holds for a single, standalone public node.
func HCloudSingleNodePublicVPN() bool {
	if ParsedGeneralConfig.Cluster.Type != constants.ClusterTypeVPN {
		return false
	}
	if !UsingHCloud() {
		return false
	}
	cp := ParsedGeneralConfig.Cloud.Hetzner.ControlPlane.HCloud
	if cp == nil || cp.Replicas != 1 {
		return false
	}
	return len(ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.HCloud) == 0
}

// Returns whether we're using Hetzner Bare Metal.
func UsingHetznerBareMetal() bool {
	if ParsedGeneralConfig.Cloud.Hetzner == nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid)
}

// Returns whether the control-plane is in Hetzner Bare Metal.
func ControlPlaneInHetznerBareMetal() bool {
	if ParsedGeneralConfig.Cloud.Hetzner == nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return mode == constants.HetznerModeBareMetal
}

// HetznerBareMetalWorkerNodeCount returns how many Hetzner bare-metal *worker*
// hosts the cluster has, summed across every bare-metal node-group.
//
// Control-plane hosts are deliberately excluded. The Rook Ceph values
// (values-rook-ceph.yaml.tmpl) place every Ceph daemon with a bare-metal
// nodeAffinity but no tolerations, so Ceph cannot schedule onto control-plane
// nodes — they carry the default node-role.kubernetes.io/control-plane:NoSchedule
// taint. Only worker nodes can host mons / OSDs, so only they count towards
// whether a healthy CephCluster can form.
func HetznerBareMetalWorkerNodeCount() int {
	hetzner := ParsedGeneralConfig.Cloud.Hetzner
	if hetzner == nil {
		return 0
	}

	count := 0
	for _, nodeGroup := range hetzner.NodeGroups.BareMetal {
		if nodeGroup == nil {
			continue
		}
		count += len(nodeGroup.BareMetalHosts)
	}
	return count
}

// RookCephEnabled reports whether kubeaid-cli should deploy Rook Ceph: only on
// Hetzner bare-metal, and only once there are at least
// constants.RookCephMinNodes bare-metal worker nodes able to host it. The
// shipped CephCluster can't become healthy on fewer hosts (see
// constants.RookCephMinNodes), so below the threshold we skip rendering and
// syncing it entirely rather than leave a permanently-unhealthy cluster.
func RookCephEnabled() bool {
	return UsingHetznerBareMetal() &&
		(HetznerBareMetalWorkerNodeCount() >= constants.RookCephMinNodes)
}
