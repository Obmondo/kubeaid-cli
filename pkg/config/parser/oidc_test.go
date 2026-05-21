// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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

// findAuthConfigFile returns the rendered AuthenticationConfiguration
// file from apiServer.Files, or nil if absent. Tests assert against
// it instead of inspecting the YAML body via string contains.
func findAuthConfigFile(t *testing.T) *config.FileConfig {
	t.Helper()

	for i, f := range config.ParsedGeneralConfig.Cluster.APIServer.Files {
		if f.Path == constants.KubeAPIServerAuthenticationConfigPath {
			return &config.ParsedGeneralConfig.Cluster.APIServer.Files[i]
		}
	}
	return nil
}

// parseRenderedAuthConfig unmarshals the YAML body kubeaid-cli emits
// so individual fields can be asserted without depending on whitespace
// or ordering.
func parseRenderedAuthConfig(t *testing.T, body string) authenticationConfig {
	t.Helper()

	var got authenticationConfig
	require.NoError(t, yaml.Unmarshal([]byte(body), &got))
	return got
}

func TestHydrateWithOIDCOptions_NoOpWhenBlockUnset(t *testing.T) {
	withFreshConfig(t, func() {
		hydrateWithOIDCOptions()

		assert.Empty(t, config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs)
		assert.Empty(t, config.ParsedGeneralConfig.Cluster.APIServer.Files)
		assert.Empty(t, config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes)
	})
}

func TestHydrateWithOIDCOptions_RendersAuthenticationConfig(t *testing.T) {
	withFreshConfig(t, func() {
		config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
			IssuerURL:     "https://keycloak.example/realms/clusters",
			ClientID:      "kubernetes-staging",
			UsernameClaim: "email",
			GroupsClaim:   "groups",
		}

		hydrateWithOIDCOptions()

		// The --authentication-config flag points at the rendered file.
		args := config.ParsedGeneralConfig.Cluster.APIServer.ExtraArgs
		assert.Equal(t,
			constants.KubeAPIServerAuthenticationConfigPath,
			args[constants.KubeAPIServerFlagAuthenticationConfig],
		)

		// File entry exists and parses to the expected structure.
		file := findAuthConfigFile(t)
		require.NotNil(t, file)
		got := parseRenderedAuthConfig(t, file.Content)

		assert.Equal(t, "apiserver.config.k8s.io/v1", got.APIVersion)
		assert.Equal(t, "AuthenticationConfiguration", got.Kind)
		require.Len(t, got.JWT, 1)

		jwt := got.JWT[0]
		assert.Equal(t, "https://keycloak.example/realms/clusters", jwt.Issuer.URL)
		assert.Equal(t, []string{"kubernetes-staging"}, jwt.Issuer.Audiences)
		assert.Empty(t, jwt.Issuer.CertificateAuthority,
			"CABundlePath unset → no inline CA")

		assert.Equal(t, "email", jwt.ClaimMappings.Username.Claim)
		assert.Empty(t, jwt.ClaimMappings.Username.Prefix)
		assert.Equal(t, "groups", jwt.ClaimMappings.Groups.Claim)
		assert.Empty(t, jwt.ClaimMappings.Groups.Prefix)

		// Mount entry plumbed through to the apiserver pod.
		vols := config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes
		require.Len(t, vols, 1)
		assert.Equal(t, constants.KubeAPIServerAuthenticationConfigPath, vols[0].HostPath)
		assert.Equal(t, constants.KubeAPIServerAuthenticationConfigPath, vols[0].MountPath)
		assert.True(t, vols[0].ReadOnly)
	})
}

func TestHydrateWithOIDCOptions_PrefixesPropagated(t *testing.T) {
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

		file := findAuthConfigFile(t)
		require.NotNil(t, file)
		got := parseRenderedAuthConfig(t, file.Content)

		assert.Equal(t, "oidc:", got.JWT[0].ClaimMappings.Username.Prefix)
		assert.Equal(t, "oidc:", got.JWT[0].ClaimMappings.Groups.Prefix)
	})
}

func TestHydrateWithOIDCOptions_CABundleEmbeddedInline(t *testing.T) {
	withFreshConfig(t, func() {
		const pem = "-----BEGIN CERTIFICATE-----\nFAKEBYTES\n-----END CERTIFICATE-----\n"

		dir := t.TempDir()
		caPath := filepath.Join(dir, "ca.pem")
		require.NoError(t, os.WriteFile(caPath, []byte(pem), 0o600))

		config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
			IssuerURL:     "https://kc.example/realms/x",
			ClientID:      "k8s",
			UsernameClaim: "email",
			GroupsClaim:   "groups",
			CABundlePath:  caPath,
		}

		hydrateWithOIDCOptions()

		file := findAuthConfigFile(t)
		require.NotNil(t, file)
		got := parseRenderedAuthConfig(t, file.Content)

		assert.Equal(t, pem, got.JWT[0].Issuer.CertificateAuthority,
			"CABundlePath contents must be embedded inline")

		// CA inlined in the YAML, so the only mount is the
		// auth-config file itself — no separate CA mount.
		vols := config.ParsedGeneralConfig.Cluster.APIServer.ExtraVolumes
		require.Len(t, vols, 1)
		assert.Equal(t, constants.KubeAPIServerAuthenticationConfigPath, vols[0].HostPath)
	})
}

func TestHydrateWithOIDCOptions_ObmondoSREAddsSecondIssuer(t *testing.T) {
	withFreshConfig(t, func() {
		config.ParsedGeneralConfig.Cluster.Name = "acme-prod"
		config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
			IssuerURL:     "https://keycloak.vpn.acme.com/realms/acme",
			ClientID:      "kubernetes-acme-prod",
			UsernameClaim: "email",
			GroupsClaim:   "groups",
		}
		config.ParsedGeneralConfig.Obmondo = &config.ObmondoConfig{Monitoring: true}

		hydrateWithOIDCOptions()

		got := parseRenderedAuthConfig(t, findAuthConfigFile(t).Content)
		require.Len(t, got.JWT, 2, "customer + Obmondo issuers must both be present")

		assert.Equal(t, "https://keycloak.vpn.acme.com/realms/acme", got.JWT[0].Issuer.URL)
		assert.Equal(t, []string{"kubernetes-acme-prod"}, got.JWT[0].Issuer.Audiences)

		assert.Equal(t, constants.ObmondoKeycloakIssuerURL, got.JWT[1].Issuer.URL)
		assert.Equal(t, []string{"acme-prod"}, got.JWT[1].Issuer.Audiences,
			"Obmondo audience derives from cluster.name")
	})
}

func TestHydrateWithOIDCOptions_ObmondoSREOnlyWhenCustomerOIDCAbsent(t *testing.T) {
	withFreshConfig(t, func() {
		config.ParsedGeneralConfig.Cluster.Name = "acme-prod"
		config.ParsedGeneralConfig.Cluster.APIServer.OIDC = nil
		config.ParsedGeneralConfig.Obmondo = &config.ObmondoConfig{Monitoring: true}

		hydrateWithOIDCOptions()

		got := parseRenderedAuthConfig(t, findAuthConfigFile(t).Content)
		require.Len(t, got.JWT, 1)
		assert.Equal(t, constants.ObmondoKeycloakIssuerURL, got.JWT[0].Issuer.URL)
	})
}

func TestHydrateWithOIDCOptions_ObmondoMonitoringOffSkipsSecondIssuer(t *testing.T) {
	withFreshConfig(t, func() {
		config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
			IssuerURL:     "https://keycloak.vpn.acme.com/realms/acme",
			ClientID:      "kubernetes-acme-prod",
			UsernameClaim: "email",
			GroupsClaim:   "groups",
		}
		config.ParsedGeneralConfig.Obmondo = &config.ObmondoConfig{Monitoring: false}

		hydrateWithOIDCOptions()

		got := parseRenderedAuthConfig(t, findAuthConfigFile(t).Content)
		require.Len(t, got.JWT, 1)
	})
}

func TestHydrateWithOIDCOptions_ReplacesOwnFileOnReHydrate(t *testing.T) {
	withFreshConfig(t, func() {
		config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
			IssuerURL:     "https://first.example/realms/x",
			ClientID:      "k8s",
			UsernameClaim: "email",
			GroupsClaim:   "groups",
		}
		hydrateWithOIDCOptions()

		// Caller flips the issuer and re-runs (e.g. day-2 edit).
		config.ParsedGeneralConfig.Cluster.APIServer.OIDC.IssuerURL = "https://second.example/realms/x"
		hydrateWithOIDCOptions()

		// Still exactly one file entry — the second hydrate replaces
		// content rather than appending a duplicate.
		var matched int
		for _, f := range config.ParsedGeneralConfig.Cluster.APIServer.Files {
			if f.Path == constants.KubeAPIServerAuthenticationConfigPath {
				matched++
			}
		}
		assert.Equal(t, 1, matched)

		file := findAuthConfigFile(t)
		require.NotNil(t, file)
		got := parseRenderedAuthConfig(t, file.Content)
		assert.Equal(t, "https://second.example/realms/x", got.JWT[0].Issuer.URL)
	})
}
