// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const authPath = "/security/user/authenticate"

func TestStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case authPath:
			_, _ = w.Write([]byte("jwt"))
		case "/agents/summary/status":
			envelope(w, map[string]any{"connection": map[string]any{"active": 7, "disconnected": 2, "never_connected": 1, "pending": 0, "total": 10}})
		case "/manager/daemons/stats":
			assert.Equal(t, "wazuh-analysisd", r.URL.Query().Get("daemons_list"))
			affected(w, []any{map[string]any{"name": "wazuh-analysisd", "metrics": map[string]any{
				"eps": map[string]any{"events_dropped": 3},
				"events": map[string]any{"received_breakdown": map[string]any{"dropped_breakdown": map[string]any{
					"agent": 4, "modules_breakdown": map[string]any{"syscheck": 5, "logcollector_breakdown": map[string]any{"others": 1}},
				}}},
				"queues": map[string]any{"alerts": map[string]any{"size": 1024, "usage": 12}, "syscheck": map[string]any{"size": 1024, "usage": 64.5}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: testUser, Password: testPass, HTTP: srv.Client()}

	a, err := c.AgentSummary(context.Background())
	require.NoError(t, err)
	assert.Equal(t, AgentSummary{Active: 7, Disconnected: 2, NeverConnected: 1, Total: 10}, a)

	s, err := c.AnalysisdStats(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 13.0, s.EventsDropped, 0.001)
	assert.InDelta(t, 64.5, s.QueueUsageMax, 0.001)
}
