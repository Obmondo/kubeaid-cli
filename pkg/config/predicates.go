// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// The predicates below answer "what shape is this cluster" from a parsed config. They live
// beside the schema deliberately: they read the GeneralConfig they are called on and nothing
// else, so they cost this package nothing. Library code calls the methods on the cluster's own
// config; the package-level functions are the CLI's shorthand for ParsedGeneralConfig.

type ReleaseDetails struct {
	TagName string `json:"tag_name"`
}

// EKSEnabled reports whether the cluster runs an AWS managed (EKS) control plane — CAPA
// AWSManagedControlPlane instead of KubeadmControlPlane. Workers remain self-managed
// MachineDeployments either way.
func (g *GeneralConfig) EKSEnabled() bool {
	return g.Cloud.AWS != nil && g.Cloud.AWS.EKS
}

// AKSEnabled reports whether the cluster runs an Azure managed (AKS) control plane — CAPZ
// AzureManagedControlPlane instead of KubeadmControlPlane, with workers as AKS agent pools
// (AzureManagedMachinePool).
func (g *GeneralConfig) AKSEnabled() bool {
	return g.Cloud.Azure != nil && g.Cloud.Azure.AKS
}

// ManagedControlPlaneEnabled reports whether the cloud provider owns the control plane (EKS or
// AKS). Such clusters have no control-plane Machines or Nodes, no KubeadmControlPlane, and get
// their CNI installed by kubeaid-cli itself instead of a postKubeadm hook.
func (g *GeneralConfig) ManagedControlPlaneEnabled() bool {
	return g.EKSEnabled() || g.AKSEnabled()
}

// UsingHCloud returns whether we're using HCloud.
func (g *GeneralConfig) UsingHCloud() bool {
	if g.Cloud.Hetzner == nil {
		return false
	}

	mode := g.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid)
}

// ControlPlaneInHCloud returns whether the control-plane is in HCloud.
func (g *GeneralConfig) ControlPlaneInHCloud() bool {
	if g.Cloud.Hetzner == nil {
		return false
	}

	mode := g.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid)
}

// CoturnFloatingIPEnabled reports whether to provision an HCloud Floating IP for NetBird Coturn
// (STUN/TURN) — plus the hcloud-fip-controller app, Coturn DaemonSet overlay, and per-CP netplan
// binding. Only multi-CP HCloud VPN clusters: Coturn can land on any CP node, so its public IP
// must float; a single CP has no failover and non-VPN clusters run no Coturn.
func (g *GeneralConfig) CoturnFloatingIPEnabled() bool {
	if g.Cluster.Type != constants.ClusterTypeVPN {
		return false
	}
	if !g.UsingHCloud() {
		return false
	}
	if g.Cloud.Hetzner.ControlPlane.HCloud == nil {
		return false
	}
	return g.Cloud.Hetzner.ControlPlane.HCloud.Replicas > 1
}

// HCloudSingleNodePublic reports whether this is the single-node public control-plane topology:
// pure-HCloud, one CP replica, no HCloud worker node-groups — any cluster.type. kubeaid-cli then
// skips the NAT gateway + control-plane LB and puts the lone node on a public IPv4, reached via
// the operator's api DNS name. Excluded: hybrid (private network to bare-metal workers) and
// workloads behind an existing VPN (hcloudVPNCluster set) — both stay private; a worker
// node-group would need the NAT gateway for egress.
func (g *GeneralConfig) HCloudSingleNodePublic() bool {
	hetzner := g.Cloud.Hetzner
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

// VPNClusterEnabled reports whether to render the VPN-cluster-wide infrastructure (cnpg,
// traefik, the netbird SealedSecrets, the postgres DSN patch): any VPN cluster with a keycloak
// block, regardless of Keycloak mode — NetBird itself runs in-cluster either way. Workload
// clusters: false.
func (g *GeneralConfig) VPNClusterEnabled() bool {
	cluster := g.Cluster
	return cluster.Type == constants.ClusterTypeVPN && cluster.Keycloak != nil
}

// ManagedKeycloakEnabled reports whether kubeaid-cli installs Keycloak itself (the keycloakx
// app, keycloak-admin SealedSecret, realm reconciler): a VPN cluster with
// keycloak.mode=managed. Nil-safe; workload clusters: false.
func (g *GeneralConfig) ManagedKeycloakEnabled() bool {
	cluster := g.Cluster
	if cluster.Type != constants.ClusterTypeVPN || cluster.Keycloak == nil {
		return false
	}
	return cluster.Keycloak.Mode == constants.KeycloakModeManaged
}

// UsingHetznerBareMetal returns whether we're using Hetzner Bare Metal.
func (g *GeneralConfig) UsingHetznerBareMetal() bool {
	if g.Cloud.Hetzner == nil {
		return false
	}

	mode := g.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid)
}

// ControlPlaneInHetznerBareMetal returns whether the control-plane is in Hetzner Bare Metal.
func (g *GeneralConfig) ControlPlaneInHetznerBareMetal() bool {
	if g.Cloud.Hetzner == nil {
		return false
	}

	mode := g.Cloud.Hetzner.Mode
	return mode == constants.HetznerModeBareMetal
}

// HetznerBareMetalWorkerNodeCount returns how many Hetzner bare-metal *worker* hosts the cluster
// has, summed across every bare-metal node-group.
//
// Control-plane hosts are deliberately excluded. The Rook Ceph values
// (values-rook-ceph.yaml.tmpl) place every Ceph daemon with a bare-metal nodeAffinity but no
// tolerations, so Ceph cannot schedule onto control-plane nodes — they carry the default
// node-role.kubernetes.io/control-plane:NoSchedule taint. Only worker nodes can host mons /
// OSDs, so only they count towards whether a healthy CephCluster can form.
func (g *GeneralConfig) HetznerBareMetalWorkerNodeCount() int {
	hetzner := g.Cloud.Hetzner
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

// RookCephEnabled reports whether kubeaid-cli should deploy Rook Ceph: only on Hetzner
// bare-metal, and only once there are at least constants.RookCephMinNodes bare-metal worker
// nodes able to host it. The shipped CephCluster can't become healthy on fewer hosts (see
// constants.RookCephMinNodes), so below the threshold we skip rendering and syncing it entirely
// rather than leave a permanently-unhealthy cluster.
func (g *GeneralConfig) RookCephEnabled() bool {
	return g.UsingHetznerBareMetal() &&
		(g.HetznerBareMetalWorkerNodeCount() >= constants.RookCephMinNodes)
}

// ObmondoIntegrationEnabled reports whether this cluster pushes to Obmondo: monitoring was asked
// for, and the mTLS material to authenticate with is on disk.
//
// Cheap on purpose — it is called from several render paths. That the certificate is a parseable
// X.509 is checked once at parse time by validateObmondoMonitoring, so by the time this runs, a
// non-empty CertPath means a real certificate.
//
// The material is what distinguishes an Obmondo customer from an opensource user: it is minted
// by the portal and written by `cluster bootstrap --token`, and the API refuses to mint one
// without an active subscription.
//
// obmondo.monitoring on its own requires no certificate. An opensource user can set it and get
// kube-prometheus, gated separately by --skip-monitoring-setup and general.kubePrometheus; they
// simply get none of the Obmondo-side wiring, which would otherwise render a SealedSecret out of
// empty paths and fail the ArgoCD sync.
func (g *GeneralConfig) ObmondoIntegrationEnabled() bool {
	obmondo := g.Obmondo
	return obmondo != nil &&
		obmondo.Monitoring &&
		obmondo.CertPath != "" &&
		obmondo.KeyPath != ""
}

// VulnerabilityScanningEnabled reports whether to render the trivy-operator and version-checker
// Apps. Both or neither: the chart's CVE alert joins a metric from each, so trivy-operator alone
// yields an alert that cannot fire.
func (g *GeneralConfig) VulnerabilityScanningEnabled() bool {
	return g.Cluster.Security.VulnerabilityScanning
}

// RuntimeDetectionEnabled reports whether to render the tetragon App.
func (g *GeneralConfig) RuntimeDetectionEnabled() bool {
	return g.Cluster.Security.RuntimeDetection
}

// NetBirdOperatorEnabled reports whether to render the netbird-operator ArgoCD app: every VPN
// cluster (it hosts Mgmt; the operator's CRDs declare routing-peer and exposed-service wiring),
// and workload clusters that join a mesh (cluster.netbird.dns set — the NetBird Management
// endpoint).
func (g *GeneralConfig) NetBirdOperatorEnabled() bool {
	cluster := g.Cluster
	if cluster.Type == constants.ClusterTypeVPN {
		return true
	}
	return cluster.Type == constants.ClusterTypeWorkload &&
		cluster.NetBird != nil && cluster.NetBird.DNS != ""
}

// NetBirdClusterProxyEnabled reports whether the netbird-operator clusterProxy block is
// configured and enabled. Nil-safe.
func (g *GeneralConfig) NetBirdClusterProxyEnabled() bool {
	nb := g.Cluster.NetBird
	return nb != nil && nb.ClusterProxy != nil && nb.ClusterProxy.Enabled
}

// NetBirdManagementURL returns the NetBird Mgmt endpoint from cluster.netbird.dns, or "" when
// unset — the values overlay then omits managementURL (the operator binary would fall back to
// NetBird Cloud) and the API-key gate's instructions cover wiring it manually.
func (g *GeneralConfig) NetBirdManagementURL() string {
	cluster := g.Cluster
	if cluster.NetBird != nil && cluster.NetBird.DNS != "" {
		return "https://" + cluster.NetBird.DNS
	}
	return ""
}

// NetBirdAPIKey returns secrets.yaml's netbird.apiKey. Nil-safe — the netbird block is optional
// and absent on most workload-cluster secrets files.
func (s *SecretsConfig) NetBirdAPIKey() string {
	if s.NetBird == nil {
		return ""
	}
	return s.NetBird.APIKey
}

// EKSEnabled is GeneralConfig.EKSEnabled for ParsedGeneralConfig.
func EKSEnabled() bool { return ParsedGeneralConfig.EKSEnabled() }

// AKSEnabled is GeneralConfig.AKSEnabled for ParsedGeneralConfig.
func AKSEnabled() bool { return ParsedGeneralConfig.AKSEnabled() }

// ManagedControlPlaneEnabled is GeneralConfig.ManagedControlPlaneEnabled for ParsedGeneralConfig.
func ManagedControlPlaneEnabled() bool { return ParsedGeneralConfig.ManagedControlPlaneEnabled() }

// UsingHCloud is GeneralConfig.UsingHCloud for ParsedGeneralConfig.
func UsingHCloud() bool { return ParsedGeneralConfig.UsingHCloud() }

// ControlPlaneInHCloud is GeneralConfig.ControlPlaneInHCloud for ParsedGeneralConfig.
func ControlPlaneInHCloud() bool { return ParsedGeneralConfig.ControlPlaneInHCloud() }

// CoturnFloatingIPEnabled is GeneralConfig.CoturnFloatingIPEnabled for ParsedGeneralConfig.
func CoturnFloatingIPEnabled() bool { return ParsedGeneralConfig.CoturnFloatingIPEnabled() }

// HCloudSingleNodePublic is GeneralConfig.HCloudSingleNodePublic for ParsedGeneralConfig.
func HCloudSingleNodePublic() bool { return ParsedGeneralConfig.HCloudSingleNodePublic() }

// VPNClusterEnabled is GeneralConfig.VPNClusterEnabled for ParsedGeneralConfig.
func VPNClusterEnabled() bool { return ParsedGeneralConfig.VPNClusterEnabled() }

// ManagedKeycloakEnabled is GeneralConfig.ManagedKeycloakEnabled for ParsedGeneralConfig.
func ManagedKeycloakEnabled() bool { return ParsedGeneralConfig.ManagedKeycloakEnabled() }

// UsingHetznerBareMetal is GeneralConfig.UsingHetznerBareMetal for ParsedGeneralConfig.
func UsingHetznerBareMetal() bool { return ParsedGeneralConfig.UsingHetznerBareMetal() }

// ControlPlaneInHetznerBareMetal is GeneralConfig.ControlPlaneInHetznerBareMetal for
// ParsedGeneralConfig.
func ControlPlaneInHetznerBareMetal() bool {
	return ParsedGeneralConfig.ControlPlaneInHetznerBareMetal()
}

// HetznerBareMetalWorkerNodeCount is GeneralConfig.HetznerBareMetalWorkerNodeCount for
// ParsedGeneralConfig.
func HetznerBareMetalWorkerNodeCount() int {
	return ParsedGeneralConfig.HetznerBareMetalWorkerNodeCount()
}

// RookCephEnabled is GeneralConfig.RookCephEnabled for ParsedGeneralConfig.
func RookCephEnabled() bool { return ParsedGeneralConfig.RookCephEnabled() }

// ObmondoIntegrationEnabled is GeneralConfig.ObmondoIntegrationEnabled for ParsedGeneralConfig.
func ObmondoIntegrationEnabled() bool { return ParsedGeneralConfig.ObmondoIntegrationEnabled() }

// VulnerabilityScanningEnabled is GeneralConfig.VulnerabilityScanningEnabled for
// ParsedGeneralConfig.
func VulnerabilityScanningEnabled() bool {
	return ParsedGeneralConfig.VulnerabilityScanningEnabled()
}

// RuntimeDetectionEnabled is GeneralConfig.RuntimeDetectionEnabled for ParsedGeneralConfig.
func RuntimeDetectionEnabled() bool { return ParsedGeneralConfig.RuntimeDetectionEnabled() }
