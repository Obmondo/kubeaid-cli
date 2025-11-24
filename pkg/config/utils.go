// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"context"
	"encoding/json"
	"net/http"
	"path"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type ReleaseDetails struct {
	TagName string `json:"tag_name"`
}

// Returns the latest KubeAid version, fetching it from GitHub.
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
	if ParsedGeneralConfig.Cloud.Hetzner != nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid)
}

// Returns whether the control-plane is in HCloud.
func ControlPlaneInHCloud() bool {
	if ParsedGeneralConfig.Cloud.Hetzner != nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeHCloud) || (mode == constants.HetznerModeHybrid)
}

// Returns whether we're using Hetzner Bare Metal.
func UsingHetznerBareMetal() bool {
	if ParsedGeneralConfig.Cloud.Hetzner != nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return (mode == constants.HetznerModeBareMetal) || (mode == constants.HetznerModeHybrid)
}

// Returns whether the control-plane is in Hetzner Bare Metal.
func ControlPlaneInHetznerBareMetal() bool {
	if ParsedGeneralConfig.Cloud.Hetzner != nil {
		return false
	}

	mode := ParsedGeneralConfig.Cloud.Hetzner.Mode
	return mode == constants.HetznerModeBareMetal
}
