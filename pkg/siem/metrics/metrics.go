// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package metrics exports the siem-reconciler's run results and the
// platform health (package health) as Prometheus metrics, and serves a
// JSON status summary for tooling (e.g. a `siem status` command):
//
//	GET /metrics   Prometheus exposition
//	GET /status    JSON: last run, last successful run, health snapshot
//	GET /healthz   liveness, always 200
//
// Every metric is prefixed kubesoc_. Labels carry component names,
// tenant codes and indexer names only.
package metrics

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/health"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// Label values.
const (
	actionOK      = "ok"
	actionChanged = "changed"
	actionSkip    = "skip"
	actionError   = "error"
	resultSuccess = "success"
	resultError   = "error"
)

// IndexerStatuses are the OpenSearch cluster health values.
var IndexerStatuses = []string{"green", "yellow", "red"}

// RunStatus summarises one reconcile run.
type RunStatus struct {
	Time            time.Time `json:"time"`
	DurationSeconds float64   `json:"durationSeconds"`
	DryRun          bool      `json:"dryRun"`
	Objects         int       `json:"objects"`
	Changes         int       `json:"changes"`
	Errors          int       `json:"errors"`
	// ErrorsByComponent lists the components that had errors.
	ErrorsByComponent map[string]int `json:"errorsByComponent,omitempty"`
}

// Status is the /status document.
type Status struct {
	LastRun     *RunStatus       `json:"lastRun,omitempty"`
	LastSuccess *time.Time       `json:"lastSuccess,omitempty"`
	Health      *health.Snapshot `json:"health,omitempty"`
	// Healthy is false when the last run had errors or anything probed
	// is down or red.
	Healthy bool `json:"healthy"`
}

// Recorder holds the metrics of one reconciler process.
type Recorder struct {
	reg *prometheus.Registry

	objects     *prometheus.GaugeVec
	runs        *prometheus.CounterVec
	lastRun     prometheus.Gauge
	lastSuccess prometheus.Gauge
	duration    prometheus.Gauge
	dryRun      prometheus.Gauge

	indexerUp     *prometheus.GaugeVec
	indexerStatus *prometheus.GaugeVec
	diskPercent   *prometheus.GaugeVec
	diskUsed      *prometheus.GaugeVec
	diskTotal     *prometheus.GaugeVec
	managerUp     *prometheus.GaugeVec
	agents        *prometheus.GaugeVec
	dropped       *prometheus.GaugeVec
	queueUsage    *prometheus.GaugeVec
	componentUp   *prometheus.GaugeVec
	lastProbe     prometheus.Gauge

	mu     sync.Mutex
	status Status
}

// New registers the metrics in a fresh registry (with the Go and
// process collectors).
func New() *Recorder {
	g := func(name, help string, labels ...string) *prometheus.GaugeVec {
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	}
	g1 := func(name, help string) prometheus.Gauge {
		return prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
	}
	r := &Recorder{
		reg:         prometheus.NewRegistry(),
		objects:     g("kubesoc_reconciler_objects", "Objects of the last run per component and action (ok, changed, skip, error).", "component", "action"),
		runs:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kubesoc_reconciler_runs_total", Help: "Reconcile runs by result (success: no object errored)."}, []string{"result"}),
		lastRun:     g1("kubesoc_reconciler_last_run_timestamp_seconds", "End of the last reconcile run."),
		lastSuccess: g1("kubesoc_reconciler_last_success_timestamp_seconds", "End of the last reconcile run without errors."),
		duration:    g1("kubesoc_reconciler_run_duration_seconds", "Duration of the last reconcile run."),
		dryRun:      g1("kubesoc_reconciler_dry_run", "1 while the reconciler only reports changes."),

		indexerUp:     g("kubesoc_indexer_up", "1 when the indexer answered the health probe.", "indexer"),
		indexerStatus: g("kubesoc_indexer_cluster_status", "Indexer cluster health: 1 for the current status (green, yellow, red).", "indexer", "status"),
		diskPercent:   g("kubesoc_indexer_disk_used_percent", "Disk usage of the indexer's fullest data node, percent.", "indexer"),
		diskUsed:      g("kubesoc_indexer_disk_used_bytes", "Disk used on the indexer's data nodes.", "indexer"),
		diskTotal:     g("kubesoc_indexer_disk_total_bytes", "Disk size of the indexer's data nodes.", "indexer"),
		managerUp:     g("kubesoc_wazuh_manager_up", "1 when the tenant's Wazuh manager API answered.", "tenant"),
		agents:        g("kubesoc_wazuh_agents", "Agents of the tenant's manager by connection status.", "tenant", "status"),
		dropped:       g("kubesoc_wazuh_analysisd_events_dropped", "Events analysisd dropped since it started (queue full or EPS limit).", "tenant"),
		queueUsage:    g("kubesoc_wazuh_analysisd_queue_usage_max", "Usage of the fullest analysisd queue as the Wazuh API reports it (percent).", "tenant"),
		componentUp:   g("kubesoc_component_up", "1 when the component's API answered (keycloak, iris, velociraptor).", "component"),
		lastProbe:     g1("kubesoc_health_last_probe_timestamp_seconds", "Time of the last health probe."),
	}
	r.reg.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		r.objects, r.runs, r.lastRun, r.lastSuccess, r.duration, r.dryRun,
		r.indexerUp, r.indexerStatus, r.diskPercent, r.diskUsed, r.diskTotal,
		r.managerUp, r.agents, r.dropped, r.queueUsage, r.componentUp, r.lastProbe,
	)
	for _, res := range []string{resultSuccess, resultError} {
		r.runs.WithLabelValues(res)
	}
	return r
}

// Registry returns the registry, for tests and extra collectors.
func (r *Recorder) Registry() *prometheus.Registry { return r.reg }

// ObserveRun records one reconcile run that ended at end.
func (r *Recorder) ObserveRun(results []report.Result, end time.Time, took time.Duration, dryRun bool) {
	counts := map[[2]string]int{}
	rs := RunStatus{Time: end.UTC(), DurationSeconds: took.Seconds(), DryRun: dryRun, Objects: len(results)}
	for _, res := range results {
		action := actionOK
		switch {
		case res.IsChange():
			action = actionChanged
			rs.Changes++
		case res.Action == report.ActionError:
			action = actionError
			rs.Errors++
			if rs.ErrorsByComponent == nil {
				rs.ErrorsByComponent = map[string]int{}
			}
			rs.ErrorsByComponent[res.Component]++
		case res.Action == report.ActionSkip:
			action = actionSkip
		}
		counts[[2]string{res.Component, action}]++
	}
	r.objects.Reset()
	for k, n := range counts {
		r.objects.WithLabelValues(k[0], k[1]).Set(float64(n))
	}
	ts := float64(end.Unix())
	r.lastRun.Set(ts)
	r.duration.Set(took.Seconds())
	r.dryRun.Set(boolFloat(dryRun))

	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.LastRun = &rs
	if rs.Errors == 0 {
		r.runs.WithLabelValues(resultSuccess).Inc()
		r.lastSuccess.Set(ts)
		t := rs.Time
		r.status.LastSuccess = &t
	} else {
		r.runs.WithLabelValues(resultError).Inc()
	}
	r.updateHealthy()
}

// ObserveHealth records a health snapshot; series of indexers,
// managers and components no longer probed are dropped.
func (r *Recorder) ObserveHealth(s health.Snapshot) {
	for _, v := range []*prometheus.GaugeVec{r.indexerUp, r.indexerStatus, r.diskPercent, r.diskUsed, r.diskTotal, r.managerUp, r.agents, r.dropped, r.queueUsage, r.componentUp} {
		v.Reset()
	}
	for _, i := range s.Indexers {
		r.indexerUp.WithLabelValues(i.Name).Set(boolFloat(i.Up))
		if i.Status != "" {
			for _, st := range IndexerStatuses {
				r.indexerStatus.WithLabelValues(i.Name, st).Set(boolFloat(st == i.Status))
			}
		}
		if i.DiskTotalBytes > 0 {
			r.diskPercent.WithLabelValues(i.Name).Set(i.DiskUsedPercent)
			r.diskUsed.WithLabelValues(i.Name).Set(i.DiskUsedBytes)
			r.diskTotal.WithLabelValues(i.Name).Set(i.DiskTotalBytes)
		}
	}
	for _, m := range s.Managers {
		r.managerUp.WithLabelValues(m.Tenant).Set(boolFloat(m.Up))
		if !m.Up {
			continue
		}
		for status, n := range map[string]int{
			"active": m.AgentsActive, "disconnected": m.AgentsDisconnected, "never_connected": m.AgentsNeverConnected,
			"pending": m.AgentsPending, "total": m.AgentsTotal,
		} {
			r.agents.WithLabelValues(m.Tenant, status).Set(float64(n))
		}
		if m.StatsAvailable {
			r.dropped.WithLabelValues(m.Tenant).Set(m.EventsDropped)
			r.queueUsage.WithLabelValues(m.Tenant).Set(m.QueueUsageMax)
		}
	}
	for _, c := range s.Components {
		r.componentUp.WithLabelValues(c.Name).Set(boolFloat(c.Up))
	}
	r.lastProbe.Set(float64(s.Time.Unix()))

	r.mu.Lock()
	defer r.mu.Unlock()
	snap := s
	r.status.Health = &snap
	r.updateHealthy()
}

// updateHealthy needs r.mu.
func (r *Recorder) updateHealthy() {
	ok := r.status.LastRun != nil && r.status.LastRun.Errors == 0
	if r.status.Health != nil {
		ok = ok && r.status.Health.Healthy()
	}
	r.status.Healthy = ok
}

// Status returns a copy of the current status.
func (r *Recorder) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Handler serves /metrics, /status and /healthz.
func (r *Recorder) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{Registry: r.reg}))
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.Status())
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
