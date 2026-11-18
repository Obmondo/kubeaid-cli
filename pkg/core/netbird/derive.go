// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package netbird holds the bootstrap-side NetBird orchestration: the
// operator API-key gate, the CNPG postgres DSN patch, and the derivation
// helpers behind the netbird-operator chart values. The NetBird status
// client lives separately in pkg/netbird.
package netbird

import (
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// OperatorEnabled reports whether to render the netbird-operator ArgoCD app
// for the parsed config; see config.GeneralConfig.NetBirdOperatorEnabled.
func OperatorEnabled() bool {
	return config.ParsedGeneralConfig.NetBirdOperatorEnabled()
}

// ClusterProxyEnabled reports whether the netbird-operator clusterProxy block
// is configured and enabled for the parsed config. Nil-safe.
func ClusterProxyEnabled() bool {
	return config.ParsedGeneralConfig.NetBirdClusterProxyEnabled()
}

// ManagementURL returns the NetBird Mgmt endpoint from cluster.netbird.dns,
// or "" when unset; see config.GeneralConfig.NetBirdManagementURL.
func ManagementURL() string {
	return config.ParsedGeneralConfig.NetBirdManagementURL()
}

// APIKey returns secrets.yaml's netbird.apiKey. Nil-safe.
func APIKey() string {
	return config.ParsedSecretsConfig.NetBirdAPIKey()
}
