// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

func newTestHetznerWithRobotServer(handler http.Handler) (*Hetzner, *httptest.Server) {
	server := httptest.NewServer(handler)
	robotClient := resty.New().
		SetBaseURL(server.URL).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json")
	return &Hetzner{
		robotClient: robotClient,
	}, server
}

func TestActivateHRobotLinuxInstallation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		serverID    string
		fingerprint string
		status      int
		body        string
		wantErr     bool
	}{
		{
			name:        "sends correct form params and succeeds on HTTP 200",
			serverID:    "12345",
			fingerprint: "ab:cd:ef",
			status:      http.StatusOK,
			body: `{"linux":{"dist":"` + constants.HBMSInstallDistributionLatestUbuntu + `",` +
				`"lang":"en","active":true,"password":"testpw",` +
				`"authorized_key":["ab:cd:ef"],"host_key":[]}}`,
			wantErr: false,
		},
		{
			name:        "HTTP 500 returns error",
			serverID:    "12345",
			fingerprint: "ab:cd:ef",
			status:      http.StatusInternalServerError,
			body:        `{"error":{"status":500,"code":"INTERNAL_ERROR"}}`,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedPath string
			var capturedFormValues map[string][]string

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				err := r.ParseForm()
				require.NoError(t, err)
				capturedFormValues = r.PostForm

				w.WriteHeader(tc.status)
				_, err = fmt.Fprint(w, tc.body)
				require.NoError(t, err)
			})

			h, server := newTestHetznerWithRobotServer(handler)
			defer server.Close()

			ctx := context.Background()
			err := h.activateHRobotLinuxInstallation(ctx, tc.serverID, tc.fingerprint)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/boot/12345/linux", capturedPath)
			assert.Equal(t,
				[]string{constants.HBMSInstallDistributionLatestUbuntu},
				capturedFormValues["dist"],
			)
			assert.NotContains(t, capturedFormValues, "arch")
			assert.Equal(t, []string{"en"}, capturedFormValues["lang"])
			assert.Equal(t, []string{"ab:cd:ef"}, capturedFormValues["authorized_key[]"])
		})
	}
}

func TestResetHBMS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		serverID string
		status   int
		body     string
		wantErr  bool
	}{
		{
			name:     "sends hw reset type and succeeds on HTTP 200",
			serverID: "12345",
			status:   http.StatusOK,
			body:     `{"reset":{"server_ip":"1.2.3.4","type":"hw"}}`,
			wantErr:  false,
		},
		{
			name:     "HTTP 500 returns error",
			serverID: "12345",
			status:   http.StatusInternalServerError,
			body:     `{"error":{"status":500,"code":"INTERNAL_ERROR"}}`,
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedPath string
			var capturedFormValues map[string][]string

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				err := r.ParseForm()
				require.NoError(t, err)
				capturedFormValues = r.PostForm

				w.WriteHeader(tc.status)
				_, err = fmt.Fprint(w, tc.body)
				require.NoError(t, err)
			})

			h, server := newTestHetznerWithRobotServer(handler)
			defer server.Close()

			ctx := context.Background()
			err := h.resetHBMS(ctx, tc.serverID)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/reset/12345", capturedPath)
			assert.Equal(t, []string{"hw"}, capturedFormValues["type"])
		})
	}
}
