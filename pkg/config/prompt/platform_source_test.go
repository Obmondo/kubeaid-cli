// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestPlatformSourceChoice(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "official source",
			url:  constants.KubeAidPublicHTTPSURL,
			want: platformSourceOfficial,
		},
		{
			name: "custom source",
			url:  "git@github.com:acme/kubeaid-platform.git",
			want: platformSourceCustom,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, platformSourceChoice(&PromptedConfig{
				KubeaidForkURL: tc.url,
			}))
		})
	}
}

func TestApplyOfficialPlatformSource(t *testing.T) {
	cfg := &PromptedConfig{
		KubeaidForkURL: "git@github.com:acme/kubeaid-platform.git",
		KubeaidVersion: "acme-release",
	}

	applyOfficialPlatformSource(cfg, &autoDetectedConfig{KubeAidVersion: "v42.0.0"})

	assert.Equal(t, constants.KubeAidPublicHTTPSURL, cfg.KubeaidForkURL)
	assert.Equal(t, "v42.0.0", cfg.KubeaidVersion)
}

func TestApplyOfficialPlatformSourceClearsCustomVersionWithoutDetection(t *testing.T) {
	cfg := &PromptedConfig{KubeaidVersion: "acme-release"}

	applyOfficialPlatformSource(cfg, nil)

	assert.Equal(t, constants.KubeAidPublicHTTPSURL, cfg.KubeaidForkURL)
	assert.Empty(t, cfg.KubeaidVersion)
}

func TestCollectPlatformSourceIfNeeded(t *testing.T) {
	original := runPlatformSourceFormFn
	t.Cleanup(func() {
		runPlatformSourceFormFn = original
	})

	calls := 0
	runPlatformSourceFormFn = func(cfg *PromptedConfig, _ *autoDetectedConfig) error {
		calls++
		cfg.KubeaidForkURL = "git@github.com:acme/kubeaid-platform.git"
		cfg.KubeaidVersion = "v1.2.3"
		return nil
	}

	session := &promptSession{
		cfg:   &PromptedConfig{},
		state: &promptState{},
	}
	require.NoError(t, session.collectPlatformSourceIfNeeded())
	assert.Equal(t, 1, calls)
	assert.True(t, session.state.PlatformSource)

	require.NoError(t, session.collectPlatformSourceIfNeeded())
	assert.Equal(t, 1, calls)
}
