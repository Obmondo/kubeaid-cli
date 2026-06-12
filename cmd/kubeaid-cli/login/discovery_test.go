// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package login

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeIssuerDiscovery(t *testing.T) {
	t.Parallel()

	t.Run("200 with matching issuer passes", func(t *testing.T) {
		t.Parallel()

		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/.well-known/openid-configuration", r.URL.Path)
			_, _ = w.Write([]byte(`{"issuer":"` + srv.URL + `"}`))
		}))
		defer srv.Close()

		assert.NoError(t, probeIssuerDiscovery(context.Background(), srv.URL))
	})

	t.Run("trailing slash on the configured URL is tolerated", func(t *testing.T) {
		t.Parallel()

		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"issuer":"` + srv.URL + `"}`))
		}))
		defer srv.Close()

		assert.NoError(t, probeIssuerDiscovery(context.Background(), srv.URL+"/"))
	})

	t.Run("issuer mismatch names the canonical issuer", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"issuer":"https://canonical.example/auth/realms/Real"}`))
		}))
		defer srv.Close()

		err := probeIssuerDiscovery(context.Background(), srv.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mismatch")
		assert.Contains(t, err.Error(), "https://canonical.example/auth/realms/Real")
	})

	t.Run("non-200 says the issuerUrl is wrong", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		err := probeIssuerDiscovery(context.Background(), srv.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
		assert.Contains(t, err.Error(), "issuerUrl")
	})

	t.Run("non-JSON body is reported", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`<html>not json</html>`))
		}))
		defer srv.Close()

		err := probeIssuerDiscovery(context.Background(), srv.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "JSON")
	})

	t.Run("DNS failure is categorised as not resolvable", func(t *testing.T) {
		t.Parallel()

		err := categoriseDiscoveryError(
			&net.DNSError{Name: "keycloak.bogus"},
			"https://keycloak.bogus/realms/x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not resolvable")
	})
}
