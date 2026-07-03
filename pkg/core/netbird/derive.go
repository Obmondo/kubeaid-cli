// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package netbird holds the bootstrap-side NetBird orchestration: the
// operator API-key gate, the CNPG postgres DSN patch, and the derivation
// helpers behind the netbird-operator chart values. The NetBird status
// client lives separately in pkg/netbird.
package netbird

import (
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// OperatorEnabled reports whether to render the netbird-operator ArgoCD app:
// every VPN cluster (it hosts Mgmt; the operator's CRDs declare routing-peer
// and exposed-service wiring), and workload clusters that join a mesh
// (cluster.netbird.dns set — the NetBird Management endpoint).
func OperatorEnabled() bool {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Type == constants.ClusterTypeVPN {
		return true
	}
	return cluster.Type == constants.ClusterTypeWorkload &&
		cluster.NetBird != nil && cluster.NetBird.DNS != ""
}

// ClusterProxyEnabled reports whether the netbird-operator clusterProxy block
// is configured and enabled. Nil-safe.
func ClusterProxyEnabled() bool {
	nb := config.ParsedGeneralConfig.Cluster.NetBird
	return nb != nil && nb.ClusterProxy != nil && nb.ClusterProxy.Enabled
}

// ManagementURL returns the NetBird Mgmt endpoint from cluster.netbird.dns,
// or "" when unset — the values overlay then omits managementURL (the
// operator binary would fall back to NetBird Cloud) and the API-key gate's
// instructions cover wiring it manually.
func ManagementURL() string {
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.NetBird != nil && cluster.NetBird.DNS != "" {
		return "https://" + cluster.NetBird.DNS
	}
	return ""
}

// APIKey returns secrets.yaml's netbird.apiKey. Nil-safe — the netbird block
// is optional and absent on most workload-cluster secrets files.
func APIKey() string {
	creds := config.ParsedSecretsConfig.NetBird
	if creds == nil {
		return ""
	}
	return creds.APIKey
}
