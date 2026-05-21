// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestResolveManagementClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		explicit      string
		targetCluster string
		want          string
	}{
		{
			name:          "explicit name is returned unchanged",
			explicit:      "my-custom-mgmt",
			targetCluster: "staging",
			want:          "my-custom-mgmt",
		},
		{
			name:          "empty explicit uses prefix + target cluster name",
			explicit:      "",
			targetCluster: "staging",
			want:          constants.ManagementClusterNamePrefix + "staging",
		},
		{
			name:          "empty explicit with different target cluster name",
			explicit:      "",
			targetCluster: "production",
			want:          constants.ManagementClusterNamePrefix + "production",
		},
		{
			name:          "explicit overrides even when target cluster name is set",
			explicit:      "override-name",
			targetCluster: "production",
			want:          "override-name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			origClusterName := config.ParsedGeneralConfig.Cluster.Name
			t.Cleanup(func() {
				config.ParsedGeneralConfig.Cluster.Name = origClusterName
			})
			config.ParsedGeneralConfig.Cluster.Name = tc.targetCluster

			got := resolveManagementClusterName(tc.explicit)
			assert.Equal(t, tc.want, got)
		})
	}
}
