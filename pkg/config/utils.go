// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package config

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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

func GetLatestK3sImageTag(ctx context.Context) (string, error) {
	defaultVersion := "v1.34.5-k3s1"
	resp, err := http.Get("https://api.github.com/repos/k3s-io/k3s/releases/latest")
	if err != nil {
		return defaultVersion, err
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return defaultVersion, err
	}

	// This is present to handle any unexpected type of error like 404 etc
	if release.TagName == "" {
		slog.InfoContext(ctx,
			"Fetched tag name is empty, using default version",
			"defaultVersion", defaultVersion,
		)
		return defaultVersion, nil
	}

	// Upstream contains "+" which we need to replace to "-" to match tags naming convention
	return strings.ReplaceAll(release.TagName, "+", "-"), nil
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
