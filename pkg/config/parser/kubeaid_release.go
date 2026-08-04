// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

// getLatestKubeAidVersion returns the latest KubeAid version, fetching it
// from GitHub.
//
// Lives here rather than beside the config schema because it makes a
// network call and asserts on failure, neither of which pkg/config can
// afford — that package is imported by obmondo-api-go to render and
// validate config, and must stay free of I/O.
//
//nolint:unused // kept deliberately; carried over unused from pkg/config.
func getLatestKubeAidVersion(ctx context.Context) string {
	response, err := http.DefaultClient.Get(
		"https://api.github.com/repos/Obmondo/KubeAid/releases/latest",
	)
	assert.AssertErrNil(ctx, err, "Failed getting KubeAid's latest release details")
	defer response.Body.Close()

	assert.Assert(
		ctx,
		(response.StatusCode == http.StatusOK),
		"Failed getting KubeAid's latest release details",
	)

	var releaseDetails config.ReleaseDetails
	err = json.NewDecoder(response.Body).Decode(&releaseDetails)
	assert.AssertErrNil(ctx, err, "Failed JSON decoding KubeAid's latest release details")

	return releaseDetails.TagName
}
