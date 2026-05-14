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

const (
	// testACMEEmail is a stand-in contact for ACME registration in
	// keycloak validator tests. Pulled out as a constant so goconst
	// stops flagging the multi-occurrence literal across test cases.
	testACMEEmail = "ops@acme.com"

	// testClusterNameVPN is the cluster name reused across the
	// hydrateKeycloakOIDC test cases.
	testClusterNameVPN = "acme-vpn"
)

func TestDeriveRealm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "single-segment TLD",
			host: "keycloak.vpn.acme.com",
			want: "acme",
		},
		{
			name: "multi-part TLD (.co.uk)",
			host: "keycloak.client.foo.co.uk",
			want: "foo",
		},
		{
			name: "another multi-part TLD (.com.au)",
			host: "kc.bar.com.au",
			want: "bar",
		},
		{
			name: "two-segment domain",
			host: "kc.acme.com",
			want: "acme",
		},
		{
			name: "bare apex domain",
			host: "acme.com",
			want: "acme",
		},
		{
			name: "empty host returns empty",
			host: "",
			want: "",
		},
		{
			name: "whitespace-only host returns empty",
			host: "   ",
			want: "",
		},
		{
			name: "host without a known public suffix returns empty",
			host: "localhost",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, deriveRealm(tc.host))
		})
	}
}

// withFreshKeycloakConfig swaps ParsedGeneralConfig for the duration of
// fn so the test never leaks state. Mirrors withFreshConfig from the
// OIDC tests but lives here to avoid an import cycle.
func withFreshKeycloakConfig(t *testing.T, fn func()) {
	t.Helper()

	orig := config.ParsedGeneralConfig
	config.ParsedGeneralConfig = &config.GeneralConfig{}

	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	fn()
}

func TestHydrateKeycloakDefaults(t *testing.T) {
	t.Run("no-op when keycloak block is unset", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			hydrateKeycloakDefaults()
			assert.Nil(t, config.ParsedGeneralConfig.Cluster.Keycloak)
		})
	})

	t.Run("derives realm from DNS when realm is empty", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode: "managed",
				DNS:  "keycloak.vpn.acme.com",
			}

			hydrateKeycloakDefaults()
			assert.Equal(t, "acme", config.ParsedGeneralConfig.Cluster.Keycloak.Realm)
		})
	})

	t.Run("preserves explicit realm override", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "managed",
				DNS:   "keycloak.vpn.acme.com",
				Realm: "customrealm",
			}

			hydrateKeycloakDefaults()
			assert.Equal(t, "customrealm",
				config.ParsedGeneralConfig.Cluster.Keycloak.Realm)
		})
	})

	t.Run("multi-part TLD derives correctly", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode: "managed",
				DNS:  "kc.foo.co.uk",
			}

			hydrateKeycloakDefaults()
			assert.Equal(t, "foo", config.ParsedGeneralConfig.Cluster.Keycloak.Realm)
		})
	})
}

func TestHydrateKeycloakOIDC(t *testing.T) {
	t.Run("derives issuer URL and client ID for managed Keycloak (VPN cluster)", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Name = testClusterNameVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  constants.KeycloakModeManaged,
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}

			hydrateKeycloakOIDC()

			oidc := config.ParsedGeneralConfig.Cluster.APIServer.OIDC
			require.NotNil(t, oidc)
			assert.Equal(t, "https://keycloak.vpn.acme.com/realms/acme", oidc.IssuerURL)
			assert.Equal(t, "kubernetes-acme-vpn", oidc.ClientID)
			assert.Equal(t, "email", oidc.UsernameClaim)
			assert.Equal(t, "groups", oidc.GroupsClaim)
		})
	})

	t.Run("derives issuer URL and client ID for external Keycloak (workload cluster)", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeWorkload
			config.ParsedGeneralConfig.Cluster.Name = "acme-staging"
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  constants.KeycloakModeExternal,
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}

			hydrateKeycloakOIDC()

			oidc := config.ParsedGeneralConfig.Cluster.APIServer.OIDC
			require.NotNil(t, oidc,
				"workload+external should also auto-derive — same parent VPN's Keycloak")
			assert.Equal(t, "https://keycloak.vpn.acme.com/realms/acme", oidc.IssuerURL)
			assert.Equal(t, "kubernetes-acme-staging", oidc.ClientID)
		})
	})

	t.Run("explicit apiServer.oidc wins over derived defaults", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Name = testClusterNameVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  constants.KeycloakModeManaged,
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}
			explicit := &config.OIDCConfig{
				IssuerURL:     "https://override.example/realms/x",
				ClientID:      "custom-client",
				UsernameClaim: "preferred_username",
				GroupsClaim:   "roles",
			}
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = explicit

			hydrateKeycloakOIDC()

			assert.Same(t, explicit, config.ParsedGeneralConfig.Cluster.APIServer.OIDC,
				"explicit OIDC block must not be replaced")
		})
	})

	t.Run("no-op when keycloak block is unset", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Name = "workload"

			hydrateKeycloakOIDC()

			assert.Nil(t, config.ParsedGeneralConfig.Cluster.APIServer.OIDC)
		})
	})

	t.Run("no-op when realm cannot be derived", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Name = testClusterNameVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode: constants.KeycloakModeManaged,
				DNS:  "localhost",
			}
			// hydrateKeycloakDefaults can't derive a realm from
			// "localhost"; validateKeycloakConfig fails with a
			// clear error later. Don't paper over it here.
			hydrateKeycloakDefaults()

			hydrateKeycloakOIDC()

			assert.Nil(t, config.ParsedGeneralConfig.Cluster.APIServer.OIDC)
		})
	})
}

func TestValidateKeycloakConfig(t *testing.T) {
	t.Run("vpn cluster without keycloak block fails", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = nil

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "required when cluster.type=vpn")
		})
	})

	t.Run("workload cluster without keycloak block is allowed", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeWorkload
			config.ParsedGeneralConfig.Cluster.Keycloak = nil

			require.NoError(t, validateKeycloakConfig())
		})
	})

	t.Run("vpn cluster with managed keycloak passes", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "managed",
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}
			config.ParsedGeneralConfig.Cluster.NetBird = &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			}
			config.ParsedGeneralConfig.Cluster.ACMEEmail = testACMEEmail

			require.NoError(t, validateKeycloakConfig())
		})
	})

	t.Run("managed keycloak without netbird DNS fails", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "managed",
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}
			config.ParsedGeneralConfig.Cluster.NetBird = nil

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cluster.netbird.dns is required")
		})
	})

	t.Run("managed keycloak without ACME email fails", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "managed",
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}
			config.ParsedGeneralConfig.Cluster.NetBird = &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			}
			config.ParsedGeneralConfig.Cluster.ACMEEmail = ""

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cluster.acmeEmail is required")
		})
	})

	t.Run("workload cluster with managed keycloak is rejected", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeWorkload
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  constants.KeycloakModeManaged,
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "only VPN clusters host Keycloak")
		})
	})

	t.Run("workload cluster with external keycloak passes", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeWorkload
			config.ParsedGeneralConfig.Cluster.Name = "acme-staging"
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  constants.KeycloakModeExternal,
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}
			// No netbird/ACME/backend-secret needed on workload —
			// those VPN-only invariants don't apply here.

			require.NoError(t, validateKeycloakConfig())
		})
	})

	t.Run("vpn cluster with external keycloak passes", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "external",
				DNS:   "auth.acme.com",
				Realm: "acme",
			}
			config.ParsedGeneralConfig.Cluster.NetBird = &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			}
			config.ParsedGeneralConfig.Cluster.ACMEEmail = testACMEEmail
			origSecrets := config.ParsedSecretsConfig
			config.ParsedSecretsConfig = &config.SecretsConfig{
				Keycloak: &config.KeycloakCredentials{
					NetBirdBackendClientSecret: "operator-supplied-secret",
				},
			}
			t.Cleanup(func() { config.ParsedSecretsConfig = origSecrets })

			require.NoError(t, validateKeycloakConfig())
		})
	})

	t.Run("external keycloak without netbird backend secret fails", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "external",
				DNS:   "auth.acme.com",
				Realm: "acme",
			}
			config.ParsedGeneralConfig.Cluster.NetBird = &config.NetBirdConfig{
				DNS: "netbird.vpn.acme.com",
			}
			config.ParsedGeneralConfig.Cluster.ACMEEmail = testACMEEmail
			origSecrets := config.ParsedSecretsConfig
			config.ParsedSecretsConfig = &config.SecretsConfig{}
			t.Cleanup(func() { config.ParsedSecretsConfig = origSecrets })

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "keycloak.netBirdBackendClientSecret is required")
		})
	})

	t.Run("invalid keycloak mode is rejected", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "weird",
				DNS:   "keycloak.vpn.acme.com",
				Realm: "acme",
			}

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be \"managed\" or \"external\"")
		})
	})

	t.Run("missing DNS fails", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "managed",
				DNS:   "",
				Realm: "acme",
			}

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "dns is required")
		})
	})

	t.Run("undeducible realm produces a clear error", func(t *testing.T) {
		withFreshKeycloakConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.Type = constants.ClusterTypeVPN
			config.ParsedGeneralConfig.Cluster.Keycloak = &config.KeycloakConfig{
				Mode:  "managed",
				DNS:   "localhost",
				Realm: "",
			}

			// Hydrate first (matches parse.go's order).
			hydrateKeycloakDefaults()

			err := validateKeycloakConfig()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not be derived from DNS")
		})
	})
}
