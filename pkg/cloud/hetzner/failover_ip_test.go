// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

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

			got, err := h.getActiveServerIP(context.Background(), tc.failoverIP)
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

// failoverHandler builds a Robot stub for the switch flow: the POST
// answers with postStatus / postBody, and each subsequent GET reports
// the next entry of activeIPs (the last entry repeats), so a test can
// model "still on the old server, then switched".
func failoverHandler(t *testing.T, postStatus int, postBody string, activeIPs []string) (http.HandlerFunc, *int32) {
	t.Helper()

	var gets int32
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/failover/192.0.2.10", r.URL.Path)

		if r.Method == http.MethodPost {
			err := r.ParseForm() //nolint:gosec
			require.NoError(t, err)
			assert.Equal(t, "10.0.0.1", r.PostFormValue("active_server_ip")) //nolint:gosec
			w.WriteHeader(postStatus)
			_, _ = fmt.Fprint(w, postBody)
			return
		}

		index := int(atomic.AddInt32(&gets, 1)) - 1
		if index >= len(activeIPs) {
			index = len(activeIPs) - 1
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"failover":{"active_server_ip":%q}}`, activeIPs[index])
	}, &gets
}

func TestPointFailoverIPTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		postStatus int
		postBody   string
		// activeIPs is what successive GETs report as the active
		// server IP; the target is always 10.0.0.1.
		activeIPs []string
		// maxWait bounds the poll loop. Defaults to a few seconds —
		// long enough for the converging cases to walk activeIPs,
		// short enough to never hang the suite.
		maxWait    time.Duration
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:       "success on HTTP 200 once Robot reports the target",
			postStatus: http.StatusOK,
			activeIPs:  []string{"10.0.0.1"},
		},
		{
			// Regression: the switch takes 90-110s, so the POST can
			// return before Robot has applied it. Poll, don't fail.
			name:       "polls until the switch lands",
			postStatus: http.StatusOK,
			activeIPs:  []string{"10.9.9.9", "10.9.9.9", "10.0.0.1"},
		},
		{
			// Regression for the bootstrap-killing
			// "unexpected status 409": a retried POST hits Robot
			// while the first one is still being applied.
			name:       "409 is resolved by polling, not treated as failure",
			postStatus: http.StatusConflict,
			postBody:   `{"error":{"status":409,"code":"FAILOVER_ALREADY_ROUTED","message":"Failover already routed"}}`,
			activeIPs:  []string{"10.0.0.1"},
		},
		{
			name:       "409 that never lands on the target still fails",
			postStatus: http.StatusConflict,
			postBody:   `{"error":{"status":409,"code":"FAILOVER_LOCKED","message":"Failover locked"}}`,
			activeIPs:  []string{"10.9.9.9"},
			maxWait:    time.Millisecond,
			wantErr:    true,
			wantErrMsg: `failover IP still points to "10.9.9.9"`,
		},
		{
			name:       "non-409 error status fails fast with Robot's message",
			postStatus: http.StatusForbidden,
			postBody:   `{"error":{"status":403,"code":"FORBIDDEN","message":"not your failover IP"}}`,
			activeIPs:  []string{"10.0.0.1"},
			wantErr:    true,
			wantErrMsg: "unexpected status 403 when pointing failover IP to server 10.0.0.1: not your failover IP",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler, _ := failoverHandler(t, tc.postStatus, tc.postBody, tc.activeIPs)
			h, server := newTestHetznerWithRobotServer(handler)
			defer server.Close()
			h.sleepFunc = noopSleep
			h.failoverMaxWait = tc.maxWait
			if h.failoverMaxWait == 0 {
				h.failoverMaxWait = 5 * time.Second
			}

			err := h.pointFailoverIPTo(context.Background(), "192.0.2.10", "10.0.0.1")
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
