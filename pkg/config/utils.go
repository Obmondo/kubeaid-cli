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
