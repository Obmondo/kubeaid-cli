// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func TestConfigFilesExist(t *testing.T) {
	original := globals.ConfigsDirectory
	t.Cleanup(func() { globals.ConfigsDirectory = original })

	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		want    bool
		wantErr bool
	}{
		{
			name: "both files present",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "general.yaml"), []byte(""), 0o600,
				))
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "secrets.yaml"), []byte(""), 0o600,
				))
				return dir
			},
			want: true,
		},
		{
			name: "general.yaml missing",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "secrets.yaml"), []byte(""), 0o600,
				))
				return dir
			},
			want: false,
		},
		{
			name: "secrets.yaml missing",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "general.yaml"), []byte(""), 0o600,
				))
				return dir
			},
			want: false,
		},
		{
			name: "neither file present",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			want: false,
		},
		{
			name: "ConfigsDirectory points at a regular file errors",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "not-a-dir")
				require.NoError(t, os.WriteFile(p, []byte(""), 0o600))
				return p
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			globals.ConfigsDirectory = tc.setup(t)

			got, err := ConfigFilesExist(globals.ConfigsDirectory)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(
					t,
					strings.Contains(err.Error(), "checking"),
					"unexpected error message: %v",
					err,
				)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDetectCloudProviderName(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.CloudConfig
		want string
	}{
		{
			name: "AWS",
			cfg:  config.CloudConfig{AWS: &config.AWSConfig{}},
			want: constants.CloudProviderAWS,
		},
		{
			name: "Azure",
			cfg:  config.CloudConfig{Azure: &config.AzureConfig{}},
			want: constants.CloudProviderAzure,
		},
		{
			name: "Hetzner",
			cfg:  config.CloudConfig{Hetzner: &config.HetznerConfig{}},
			want: constants.CloudProviderHetzner,
		},
		{
			name: "BareMetal",
			cfg:  config.CloudConfig{BareMetal: &config.BareMetalConfig{}},
			want: constants.CloudProviderBareMetal,
		},
		{
			name: "Local",
			cfg:  config.CloudConfig{Local: &config.LocalConfig{}},
			want: constants.CloudProviderLocal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalCfg := config.ParsedGeneralConfig
			originalProvider := globals.CloudProviderName
			t.Cleanup(func() {
				config.ParsedGeneralConfig = originalCfg
				globals.CloudProviderName = originalProvider
			})

			config.ParsedGeneralConfig = &config.GeneralConfig{Cloud: tc.cfg}
			globals.CloudProviderName = ""

			detectCloudProviderName()
			assert.Equal(t, tc.want, globals.CloudProviderName)
		})
	}
}

func TestHydrateCABundle(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) string
		wantBundle []byte
		wantPanic  bool
	}{
		{
			name:       "empty CABundlePath is a no-op",
			setup:      func(t *testing.T) string { return "" },
			wantBundle: nil,
		},
		{
			name: "valid CABundlePath populates the bundle",
			setup: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "ca.pem")
				require.NoError(t, os.WriteFile(p, []byte("-----BEGIN CERT-----"), 0o600))
				return p
			},
			wantBundle: []byte("-----BEGIN CERT-----"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalCfg := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = originalCfg })

			config.ParsedGeneralConfig = &config.GeneralConfig{
				Git: config.GitConfig{CABundlePath: tc.setup(t)},
			}

			hydrateCABundle(context.Background())
			assert.Equal(t, tc.wantBundle, config.ParsedGeneralConfig.Git.CABundle)
		})
	}
}
