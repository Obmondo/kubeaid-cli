// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestClientSettings(t *testing.T) {
	caBundle := []byte("-----BEGIN CERTIFICATE-----")
	extraKnownHosts := []string{"git.demo.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5"}

	tests := []struct {
		name           string
		client         *Client
		wantCABundle   []byte
		wantExtraHosts []string
	}{
		{
			name:   "a nil client",
			client: nil,
		},
		{
			name:   "a client without a config",
			client: New(nil),
		},
		{
			name:   "a config without a general config",
			client: New(&config.Config{}),
		},
		{
			name: "a config with Git settings",
			client: New(&config.Config{
				General: &config.GeneralConfig{
					Git: config.GitConfig{CABundle: caBundle, KnownHosts: extraKnownHosts},
				},
			}),
			wantCABundle:   caBundle,
			wantExtraHosts: extraKnownHosts,
		},
	}

	commonKnownHosts := New(nil).getKnownHosts()
	require.NotEmpty(t, commonKnownHosts)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCABundle, tc.client.caBundle())

			knownHosts := tc.client.getKnownHosts()
			require.GreaterOrEqual(t, len(knownHosts), len(commonKnownHosts))
			assert.Equal(t, commonKnownHosts, knownHosts[:len(commonKnownHosts)])
			assert.ElementsMatch(t, tc.wantExtraHosts, knownHosts[len(commonKnownHosts):])
		})
	}
}
