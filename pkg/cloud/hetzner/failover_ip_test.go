// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetActiveServerIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		failoverIP string
		handler    http.HandlerFunc
		wantIP     string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "success returns active server IP",
			failoverIP: "192.0.2.10",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/failover/192.0.2.10", r.URL.Path)
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"failover":{"active_server_ip":"10.0.0.1"}}`)
			},
			wantIP: "10.0.0.1",
		},
		{
			name:       "HTTP error status returns error",
			failoverIP: "192.0.2.10",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 500",
		},
		{
			name:       "invalid JSON returns unmarshal error",
			failoverIP: "192.0.2.10",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{invalid`)
			},
			wantErr:    true,
			wantErrMsg: "unmarshalling failover IP details",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			got, err := h.getActiveServerIP(tc.failoverIP)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantIP, got)
		})
	}
}

func TestPointFailoverIPTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		failoverIP     string
		targetServerIP string
		handler        http.HandlerFunc
		wantErr        bool
		wantErrMsg     string
	}{
		{
			name:           "success on HTTP 200",
			failoverIP:     "192.0.2.10",
			targetServerIP: "10.0.0.1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/failover/192.0.2.10", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				err := r.ParseForm() //nolint:gosec
				require.NoError(t, err)
				assert.Equal(t, "10.0.0.1", r.PostFormValue("active_server_ip")) //nolint:gosec
				w.WriteHeader(http.StatusOK)
			},
		},
		{
			name:           "HTTP error status returns error",
			failoverIP:     "192.0.2.10",
			targetServerIP: "10.0.0.1",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			wantErr:    true,
			wantErrMsg: "unexpected status 403",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			err := h.pointFailoverIPTo(context.Background(), tc.failoverIP, tc.targetServerIP)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
