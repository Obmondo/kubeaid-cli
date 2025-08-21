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

func IsUsingHCloud() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHCloud) ||
		(ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHybrid)
}

func IsControlPlaneInHCloud() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHCloud) ||
		(ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHybrid)
}

func IsUsingHetznerBareMetal() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeBareMetal) ||
		(ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeHybrid)
}

func IsControlPlaneInHetznerBareMetal() bool {
	return (ParsedGeneralConfig.Cloud.Hetzner.Mode == constants.HetznerModeBareMetal)
}
