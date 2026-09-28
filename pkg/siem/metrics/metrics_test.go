// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/health"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const compIRIS = "iris"

func TestObserveRun(t *testing.T) {
	r := New()
	end := time.Unix(1_800_000_000, 0)
	r.ObserveRun([]report.Result{
		{Component: compIRIS, Action: report.ActionOK},
		{Component: compIRIS, Action: report.ActionCreate},
		{Component: "retention/001", Action: report.ActionUpdate},
		{Component: "wazuh/001", Action: report.ActionError},
	}, end, 3*time.Second, false)

	assert.InDelta(t, 1, testutil.ToFloat64(r.objects.WithLabelValues(compIRIS, actionChanged)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(r.objects.WithLabelValues("wazuh/001", actionError)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(r.runs.WithLabelValues(resultError)), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(r.lastSuccess), 0)
	assert.InDelta(t, 3, testutil.ToFloat64(r.duration), 0)
	st := r.Status()
	require.NotNil(t, st.LastRun)
	assert.Equal(t, 2, st.LastRun.Changes)
	assert.Equal(t, map[string]int{"wazuh/001": 1}, st.LastRun.ErrorsByComponent)
	assert.False(t, st.Healthy)

	r.ObserveRun([]report.Result{{Component: compIRIS, Action: report.ActionOK}}, end.Add(time.Minute), time.Second, true)
	assert.InDelta(t, float64(end.Add(time.Minute).Unix()), testutil.ToFloat64(r.lastSuccess), 0)
	// The previous run's error series is gone.
	assert.Equal(t, 1, testutil.CollectAndCount(r.objects))
	assert.True(t, r.Status().Healthy)
}

func TestObserveHealthAndHandler(t *testing.T) {
	r := New()
	r.ObserveRun(nil, time.Now(), time.Second, false)
	r.ObserveHealth(health.Snapshot{
		Time: time.Now(),
		Indexers: []health.Indexer{
			{Name: "001", Up: true, Status: "yellow", DiskUsedPercent: 81, DiskUsedBytes: 81, DiskTotalBytes: 100},
			{Name: "central", Error: "connection refused"},
		},
		Managers:   []health.Manager{{Tenant: "001", Up: true, AgentsActive: 5, AgentsDisconnected: 2, AgentsTotal: 7, StatsAvailable: true, EventsDropped: 9}},
		Components: []health.Component{{Name: compIRIS, Up: true}},
	})
	assert.InDelta(t, 1, testutil.ToFloat64(r.indexerStatus.WithLabelValues("001", "yellow")), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(r.indexerStatus.WithLabelValues("001", "red")), 0)
	assert.InDelta(t, 81, testutil.ToFloat64(r.diskPercent.WithLabelValues("001")), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(r.indexerUp.WithLabelValues("central")), 0)
	assert.InDelta(t, 2, testutil.ToFloat64(r.agents.WithLabelValues("001", "disconnected")), 0)
	assert.InDelta(t, 9, testutil.ToFloat64(r.dropped.WithLabelValues("001")), 0)
	assert.False(t, r.Status().Healthy, "central indexer down")

	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()
	var body strings.Builder
	_, _ = io.Copy(&body, resp.Body)
	assert.Contains(t, body.String(), `kubesoc_indexer_disk_used_percent{indexer="001"} 81`)
	assert.Contains(t, body.String(), `kubesoc_reconciler_runs_total{result="success"} 1`)

	resp2, err := http.Get(srv.URL + "/status")
	require.NoError(t, err)
	defer resp2.Body.Close()
	var st Status
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&st))
	require.NotNil(t, st.Health)
	assert.Len(t, st.Health.Indexers, 2)
	assert.False(t, st.Healthy)
}
