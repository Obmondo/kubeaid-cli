// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

func TestValidateOIDCDiscovery(t *testing.T) {
	// t.Setenv-free, but we mutate ParsedGeneralConfig — keep
	// sequential to avoid racing siblings.

	t.Run("no-op when oidc block is unset", func(t *testing.T) {
		withFreshConfig(t, func() {
			err := ValidateOIDCDiscovery(context.Background())
			require.NoError(t, err)
		})
	})

	t.Run("issuer responding with matching JSON discovery doc succeeds", func(t *testing.T) {
		var srvURL string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/.well-known/openid-configuration" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"issuer":"` + srvURL + `"}`))
		}))
		defer srv.Close()
		srvURL = srv.URL

		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL: srv.URL,
				ClientID:  "k8s",
			}

			require.NoError(t, ValidateOIDCDiscovery(context.Background()))
		})
	})

	t.Run("trailing slash on issuer is normalised against discovery doc", func(t *testing.T) {
		var srvURL string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"issuer":"` + srvURL + `"}`))
		}))
		defer srv.Close()
		srvURL = srv.URL

		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL: srv.URL + "/", // trailing slash
				ClientID:  "k8s",
			}

			require.NoError(t, ValidateOIDCDiscovery(context.Background()))
		})
	})

	t.Run("issuer mismatch in discovery doc is reported", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"issuer":"https://wrong.example/realms/x"}`))
		}))
		defer srv.Close()

		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL: srv.URL,
				ClientID:  "k8s",
			}

			err := ValidateOIDCDiscovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "issuer mismatch")
		})
	})

	t.Run("non-200 response from issuer is reported", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}))
		defer srv.Close()

		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL: srv.URL,
				ClientID:  "k8s",
			}

			err := ValidateOIDCDiscovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "404")
		})
	})

	t.Run("garbage JSON in discovery doc is reported", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{not valid"))
		}))
		defer srv.Close()

		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL: srv.URL,
				ClientID:  "k8s",
			}

			err := ValidateOIDCDiscovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not valid JSON")
		})
	})

	t.Run("DNS lookup failure produces hostname-not-resolvable message", func(t *testing.T) {
		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				// .invalid is reserved (RFC 2606) and never resolves.
				IssuerURL: "https://no-such-host.invalid/realms/x",
				ClientID:  "k8s",
			}

			err := ValidateOIDCDiscovery(context.Background())
			require.Error(t, err)
			assert.True(t,
				strings.Contains(err.Error(), "not resolvable") ||
					strings.Contains(err.Error(), "no such host"),
				"expected DNS-failure category, got: %s", err.Error())
		})
	})

	t.Run("malformed CA bundle returns explicit error", func(t *testing.T) {
		caPath := writeTempFile(t, "not a pem")

		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL:    "https://example.com",
				ClientID:     "k8s",
				CABundlePath: caPath,
			}

			err := ValidateOIDCDiscovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no valid PEM certs")
		})
	})

	t.Run("missing CA bundle path returns explicit error", func(t *testing.T) {
		withFreshConfig(t, func() {
			config.ParsedGeneralConfig.Cluster.APIServer.OIDC = &config.OIDCConfig{
				IssuerURL:    "https://example.com",
				ClientID:     "k8s",
				CABundlePath: "/nonexistent/ca.pem",
			}

			err := ValidateOIDCDiscovery(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "reading OIDC CA bundle")
		})
	})
}

// writeTempFile drops content into a fresh file under t.TempDir and
// returns the path. Cleanup is automatic via t.TempDir.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}
