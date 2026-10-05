// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestKubeAidLicenseSummaryLines(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want []string
	}{
		{
			name: "default Obmondo source",
			url:  constants.KubeAidPublicHTTPSURL,
			want: []string{"  License:       Obmondo/KubeAid is AGPL-3.0 - review before bootstrap"},
		},
		{
			name: "custom source",
			url:  "git@github.com:example/platform.git",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, kubeAidLicenseSummaryLines(&PromptedConfig{
				KubeaidForkURL: tc.url,
			}))
		})
	}
}
