// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Obmondo/kubeaid-cli/pkg/keycloak"
)

func TestExpectStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    int
		got     int
		wantErr bool
	}{
		{"match 200", http.StatusOK, http.StatusOK, false},
		{"match 401", http.StatusUnauthorized, http.StatusUnauthorized, false},
		{"mismatch 502 vs 401", http.StatusUnauthorized, http.StatusBadGateway, true},
		{"mismatch 500 vs 200", http.StatusOK, http.StatusInternalServerError, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{StatusCode: tc.got}
			err := expectStatus(tc.want)(resp)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateKeycloakOpenIDConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{
			name:   "scope present",
			status: http.StatusOK,
			body: `{"scopes_supported":["openid","profile","email","offline_access","` +
				keycloak.NetBirdAPIScopeName + `"]}`,
			wantErr: "",
		},
		{
			name:    "scope missing — reconciler did not finish",
			status:  http.StatusOK,
			body:    `{"scopes_supported":["openid","profile","email","offline_access"]}`,
			wantErr: "missing the netbird api scope",
		},
		{
			name:    "non-200 status — realm down",
			status:  http.StatusNotFound,
			body:    `404 not found`,
			wantErr: "HTTP 404",
		},
		{
			name:    "malformed JSON",
			status:  http.StatusOK,
			body:    `not json`,
			wantErr: "decoding OpenID config",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{
				StatusCode: tc.status,
				Body:       http.NoBody,
			}
			if tc.body != "" {
				resp.Body = httpBody(tc.body)
			}

			err := validateKeycloakOpenIDConfig(resp)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestEndpointCheckRetries asserts the retry loop stops after the first
// successful attempt — so a flaky endpoint that recovers on attempt N
// doesn't continue burning the full ~1-minute budget.
func TestEndpointCheckRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := endpointCheck{
		label:    "test",
		url:      srv.URL,
		validate: expectStatus(http.StatusUnauthorized),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shortened interval is built into the production const; this test
	// just confirms the retry loop converges on the success case
	// without needing to override it.
	if err := c.runWithRetry(ctx, &http.Client{Timeout: 2 * time.Second}); err != nil {
		t.Fatalf("expected eventual success, got: %v", err)
	}
	if hits != 2 {
		t.Fatalf("expected 2 hits before success, got %d", hits)
	}
}

func httpBody(s string) closableReader {
	return closableReader{s: strings.NewReader(s)}
}

type closableReader struct {
	s *strings.Reader
}

func (r closableReader) Read(p []byte) (int, error) { return r.s.Read(p) }
func (r closableReader) Close() error               { return nil }
