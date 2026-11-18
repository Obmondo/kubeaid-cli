// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package hetzner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func TestSetupDisasterRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantErrMsg string
	}{
		{
			name:       "always returns not implemented error",
			wantErrMsg: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &Hetzner{}
			err := h.SetupDisasterRecovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}

// hetznerTestConfig returns a config over a Hetzner block of the given mode, which is left out
// when mode is empty, and the given credentials.
func hetznerTestConfig(mode string, credentials *config.HetznerCredentials) *config.Config {
	general := &config.GeneralConfig{}
	if mode != "" {
		general.Cloud.Hetzner = &config.HetznerConfig{Mode: mode}
	}

	return &config.Config{
		General: general,
		Secrets: &config.SecretsConfig{Hetzner: credentials},
	}
}

func TestNewHetznerCloudProvider(t *testing.T) {
	t.Parallel()

	hcloudCredentials := &config.HetznerCredentials{APIToken: "demo-token"}
	robotCredentials := &config.HetznerCredentials{
		Robot: &config.HetznerRobotCredentials{User: "demo", Password: "demo-password"},
	}
	allCredentials := &config.HetznerCredentials{
		APIToken: "demo-token",
		Robot:    robotCredentials.Robot,
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr string
	}{
		{
			name:    "rejects a nil config",
			cfg:     nil,
			wantErr: "general and secrets",
		},
		{
			name:    "rejects a config without a general config",
			cfg:     &config.Config{Secrets: &config.SecretsConfig{}},
			wantErr: "general and secrets",
		},
		{
			name:    "rejects a config without a secrets config",
			cfg:     &config.Config{General: &config.GeneralConfig{}},
			wantErr: "general and secrets",
		},
		{
			name:    "rejects a config without a cloud.hetzner block",
			cfg:     hetznerTestConfig("", hcloudCredentials),
			wantErr: "cloud.hetzner is required",
		},
		{
			name:    "rejects a mode that needs no client",
			cfg:     hetznerTestConfig("dedicated", allCredentials),
			wantErr: `unsupported cloud.hetzner.mode "dedicated"`,
		},
		{
			name:    "rejects hcloud without secrets.hetzner",
			cfg:     hetznerTestConfig(constants.HetznerModeHCloud, nil),
			wantErr: "secrets.hetzner is required",
		},
		{
			name:    "rejects bare-metal without the Robot credentials",
			cfg:     hetznerTestConfig(constants.HetznerModeBareMetal, hcloudCredentials),
			wantErr: "secrets.hetzner.robot is required",
		},
		{
			name: "builds for hcloud",
			cfg:  hetznerTestConfig(constants.HetznerModeHCloud, hcloudCredentials),
		},
		{
			name: "builds for bare-metal",
			cfg:  hetznerTestConfig(constants.HetznerModeBareMetal, robotCredentials),
		},
		{
			name: "builds for hybrid",
			cfg:  hetznerTestConfig(constants.HetznerModeHybrid, allCredentials),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, err := NewHetznerCloudProvider(tc.cfg)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, provider)
				return
			}

			require.NoError(t, err)
			assert.Same(t, tc.cfg, provider.cfg)
		})
	}
}

func TestProvisionPrerequisiteInfrastructureNeedsAStoragePlanApprover(t *testing.T) {
	t.Parallel()

	h := &Hetzner{cfg: hetznerTestConfig(constants.HetznerModeBareMetal, nil)}

	err := h.ProvisionPrerequisiteInfrastructure(context.Background())

	require.ErrorIs(t, err, errNoStoragePlanApprover)
}
