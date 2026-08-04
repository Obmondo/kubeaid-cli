// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// The predicates below answer "what shape is this cluster" from the parsed
// config. They live beside the schema deliberately: they read
// ParsedGeneralConfig and nothing else, so they cost this package nothing,
// and config.UsingHCloud() reads better at a call site than any package
// this could be split into.

type ReleaseDetails struct {
	TagName string `json:"tag_name"`
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

// CoturnFloatingIPEnabled reports whether to provision an HCloud Floating IP
// for NetBird Coturn (STUN/TURN) — plus the hcloud-fip-controller app, Coturn
// DaemonSet overlay, and per-CP netplan binding. Only multi-CP HCloud VPN
// clusters: Coturn can land on any CP node, so its public IP must float;
// a single CP has no failover and non-VPN clusters run no Coturn.
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

// HCloudSingleNodePublic reports whether this is the single-node public
// control-plane topology: pure-HCloud, one CP replica, no HCloud worker
// node-groups — any cluster.type. kubeaid-cli then skips the NAT gateway +
// control-plane LB and puts the lone node on a public IPv4, reached via the
// operator's api DNS name. Excluded: hybrid (private network to bare-metal
// workers) and workloads behind an existing VPN (hcloudVPNCluster set) — both
// stay private; a worker node-group would need the NAT gateway for egress.
func HCloudSingleNodePublic() bool {
	hetzner := ParsedGeneralConfig.Cloud.Hetzner
	if hetzner == nil || hetzner.Mode != constants.HetznerModeHCloud {
		return false
	}
	if hetzner.HCloudVPNCluster != nil {
		return false
	}
	cp := hetzner.ControlPlane.HCloud
	if cp == nil || cp.Replicas != 1 {
		return false
	}
	return len(hetzner.NodeGroups.HCloud) == 0
}

// VPNClusterEnabled reports whether to render the VPN-cluster-wide
// infrastructure (cnpg, traefik, the netbird SealedSecrets, the postgres DSN
// patch): any VPN cluster with a keycloak block, regardless of Keycloak mode —
// NetBird itself runs in-cluster either way. Workload clusters: false.
func VPNClusterEnabled() bool {
	cluster := ParsedGeneralConfig.Cluster
	return cluster.Type == constants.ClusterTypeVPN && cluster.Keycloak != nil
}

// ManagedKeycloakEnabled reports whether kubeaid-cli installs Keycloak itself
// (the keycloakx app, keycloak-admin SealedSecret, realm reconciler): a VPN
// cluster with keycloak.mode=managed. Nil-safe; workload clusters: false.
func ManagedKeycloakEnabled() bool {
	cluster := ParsedGeneralConfig.Cluster
	if cluster.Type != constants.ClusterTypeVPN || cluster.Keycloak == nil {
		return false
	}
	return cluster.Keycloak.Mode == constants.KeycloakModeManaged
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

// ObmondoIntegrationEnabled reports whether this cluster pushes to Obmondo:
// monitoring was asked for, and the mTLS material to authenticate with is on
// disk.
//
// Cheap on purpose — it is called from several render paths. That the
// certificate is a parseable X.509 is checked once at parse time by
// validateObmondoMonitoring, so by the time this runs, a non-empty CertPath
// means a real certificate.
//
// The material is what distinguishes an Obmondo customer from an opensource
// user: it is minted by the portal and written by `cluster bootstrap
// --token`, and the API refuses to mint one without an active subscription.
//
// obmondo.monitoring on its own requires no certificate. An opensource user
// can set it and get kube-prometheus, gated separately by
// --skip-monitoring-setup and general.kubePrometheus; they simply get none
// of the Obmondo-side wiring, which would otherwise render a SealedSecret
// out of empty paths and fail the ArgoCD sync.
func ObmondoIntegrationEnabled() bool {
	obmondo := ParsedGeneralConfig.Obmondo
	return obmondo != nil &&
		obmondo.Monitoring &&
		obmondo.CertPath != "" &&
		obmondo.KeyPath != ""
}
