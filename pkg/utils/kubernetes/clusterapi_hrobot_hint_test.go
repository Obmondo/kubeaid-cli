// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestAnnotateHRobotNodeWait(t *testing.T) {
	saved := config.ParsedGeneralConfig
	t.Cleanup(func() { config.ParsedGeneralConfig = saved })
	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedGeneralConfig.Cluster.Name = "kcm"

	testCases := []struct {
		name        string
		clusterName string
		detail      string
		wantSame    bool
		wantHas     string
	}{
		{
			name:    "bare-metal node wait names the server to rename",
			detail:  "NodeHealthy: Waiting for a Node with spec.providerID hrobot://1866482 to exist",
			wantHas: "rename server 1866482 to bm-kcm-1866482",
		},
		{
			name:        "the hint carries the cluster being bootstrapped",
			clusterName: "kam",
			detail:      "NodeHealthy: Waiting for a Node with spec.providerID hrobot://2852588 to exist",
			wantHas:     "rename server 2852588 to bm-kam-2852588",
		},
		{
			name:    "a providerID ending the line is still annotated",
			detail:  "Waiting for a Node with spec.providerID hrobot://1866484",
			wantHas: "rename server 1866484 to bm-kcm-1866484",
		},
		{
			name:     "the marker with no server id is left alone",
			detail:   "Waiting for a Node with spec.providerID hrobot:// to exist",
			wantSame: true,
		},
		{
			name:     "an unrelated status is untouched",
			detail:   "NodeHealthy: Waiting for the Machine to report a NodeRef",
			wantSame: true,
		},
		{
			name:     "a cloud providerID is untouched",
			detail:   "Waiting for a Node with spec.providerID hcloud://12345 to exist",
			wantSame: true,
		},
		{
			name:     "Ready passes straight through",
			detail:   statusReady,
			wantSame: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := testCase.clusterName
			if clusterName == "" {
				clusterName = "kcm"
			}
			config.ParsedGeneralConfig.Cluster.Name = clusterName

			got := annotateHRobotNodeWait(testCase.detail)

			if testCase.wantSame {
				assert.Equal(t, testCase.detail, got)
				return
			}
			assert.Contains(t, got, testCase.detail)
			assert.Contains(t, got, testCase.wantHas)
		})
	}
}
