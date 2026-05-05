// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// withFreshConfig swaps ParsedGeneralConfig for the duration of fn so
// the test never leaks state into siblings. Mirrors the audit_logging
// test pattern but using a function literal so the cleanup is local.
func withFreshConfig(t *testing.T, fn func()) {
	t.Helper()

	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}
	config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs = map[string]string{}

	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	fn()
}

func TestHydrateWithOIDCOptions(t *testing.T) {
	t.Run("no-op when oidc block is unset", func(t *testing.T) {
		withFreshConfig(t, func() {
			hydrateWithOIDCOptions()
			assert.Empty(t, config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs)
			assert.Empty(t, config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes)
		})
	})

	t.Run("required + defaulted fields land in extraArgs", func(t *testing.T) {
		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL:     "https://keycloak.example/realms/clusters",
				ClientID:      "kubernetes-staging",
				UsernameClaim: "email",
				GroupsClaim:   "groups",
			}

			hydrateWithOIDCOptions()

			args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
			assert.Equal(t, "https://keycloak.example/realms/clusters",
				args[constants.KubeAPIServerFlagOIDCIssuerURL])
			assert.Equal(t, "kubernetes-staging",
				args[constants.KubeAPIServerFlagOIDCClientID])
			assert.Equal(t, "email", args[constants.KubeAPIServerFlagOIDCUsernameClaim])
			assert.Equal(t, "groups", args[constants.KubeAPIServerFlagOIDCGroupsClaim])

			// Optional prefixes not set → flags absent.
			_, hasUserPrefix := args[constants.KubeAPIServerFlagOIDCUsernamePrefix]
			_, hasGroupsPrefix := args[constants.KubeAPIServerFlagOIDCGroupsPrefix]
			assert.False(t, hasUserPrefix)
			assert.False(t, hasGroupsPrefix)

			// CABundlePath unset → no volume mount, no --oidc-ca-file.
			_, hasCAFile := args[constants.KubeAPIServerFlagOIDCCAFile]
			assert.False(t, hasCAFile)
			assert.Empty(t, config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes)
		})
	})

	t.Run("optional username and groups prefix are emitted when set", func(t *testing.T) {
		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL:      "https://kc.example/realms/x",
				ClientID:       "k8s",
				UsernameClaim:  "email",
				GroupsClaim:    "groups",
				UsernamePrefix: "oidc:",
				GroupsPrefix:   "oidc:",
			}

			hydrateWithOIDCOptions()

			args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
			assert.Equal(t, "oidc:", args[constants.KubeAPIServerFlagOIDCUsernamePrefix])
			assert.Equal(t, "oidc:", args[constants.KubeAPIServerFlagOIDCGroupsPrefix])
		})
	})

	t.Run("caBundlePath drives both --oidc-ca-file and an extraVolume mount", func(t *testing.T) {
		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL:     "https://kc.example/realms/x",
				ClientID:      "k8s",
				UsernameClaim: "email",
				GroupsClaim:   "groups",
				CABundlePath:  "/etc/ssl/certs/keycloak-ca.pem",
			}

			hydrateWithOIDCOptions()

			args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
			assert.Equal(t, oidcCAFileMountPath, args[constants.KubeAPIServerFlagOIDCCAFile])

			vols := config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes
			require.Len(t, vols, 1)
			assert.Equal(t, "/etc/ssl/certs/keycloak-ca.pem", vols[0].HostPath)
			assert.Equal(t, oidcCAFileMountPath, vols[0].MountPath)
			assert.True(t, vols[0].ReadOnly)
		})
	})

	t.Run("typed OIDC fields override pre-existing ExtraArgs entries", func(t *testing.T) {
		withFreshConfig(t, func() {
			// User has stale --oidc-issuer-url in ExtraArgs from
			// before they switched to the typed block.
			config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs[constants.KubeAPIServerFlagOIDCIssuerURL] = "https://OLD.example/realms/x"

			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL:     "https://NEW.example/realms/x",
				ClientID:      "k8s",
				UsernameClaim: "email",
				GroupsClaim:   "groups",
			}

			hydrateWithOIDCOptions()

			args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
			// Typed config wins.
			assert.Equal(t, "https://NEW.example/realms/x",
				args[constants.KubeAPIServerFlagOIDCIssuerURL])
		})
	})
}
