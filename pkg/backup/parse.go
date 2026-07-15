// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"encoding/json"
	"fmt"
	"sort"
)

// BackupsResponse is the top-level /backups payload. Only the exporter's
// own metric families are present (names starting with postgres_, cnpg_
// or backup_exporter_); Go runtime/process/promhttp families are excluded
// by the exporter.
type BackupsResponse struct {
	GeneratedAt string         `json:"generated_at"`
	Metrics     []MetricFamily `json:"metrics"`
}

// MetricFamily is one Prometheus gauge family with its current samples.
type MetricFamily struct {
	Name    string         `json:"name"`
	Help    string         `json:"help"`
	Type    string         `json:"type"`
	Samples []MetricSample `json:"samples"`
}

// MetricSample is a single time series: a set of label values and the
// gauge value. Age gauges are expressed in seconds.
type MetricSample struct {
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

// ageCell holds an age/interval gauge value in seconds. Present
// distinguishes a genuine zero age from a missing series (the exporter
// may omit a family that has no current time series).
type ageCell struct {
	Seconds float64
	Present bool
}

// healthState is the derived health of a backup method or row.
type healthState int

// backupColumn is the LOGICAL or WAL half of a PostgreSQL row.
type backupColumn struct {
	Last   ageCell
	Oldest ageCell
	MaxGap ageCell
	Status healthState
}

// PostgresRow is one (namespace, cluster_name) group with its logical
// and WAL backup columns.
type PostgresRow struct {
	Namespace   string
	ClusterName string
	Logical     backupColumn
	WAL         backupColumn
}

// VeleroRow is one (namespace, resource_name, resource_type, backup)
// group. Method is the "backup" label value.
type VeleroRow struct {
	Namespace    string
	ResourceName string
	ResourceType string
	Method       string
	Last         ageCell
	Oldest       ageCell
	MaxGap       ageCell
	Status       healthState
}

// Report is the parsed, grouped view of a /backups payload.
type Report struct {
	GeneratedAt  string
	Postgres     []PostgresRow
	Velero       []VeleroRow
	VeleroErrors []string
}

// Metric family names emitted by the exporter.
const (
	metricLogicalMaxInterval = "postgres_logical_backup_max_interval"
	metricLogicalLatestAge   = "postgres_latest_logical_backup_age"
	metricLogicalOldestAge   = "postgres_oldest_logical_backup_age"
	metricWALMaxInterval     = "cnpg_wal_backup_max_interval"
	metricWALLatestAge       = "postgres_latest_cnpg_wal_backup_age"
	metricWALOldestAge       = "postgres_oldest_cnpg_wal_backup_age"
	metricPostgresError      = "backup_exporter_postgres_error"
	metricVeleroLatestAge    = "backup_exporter_velero_latest_backup_age"
	metricVeleroOldestAge    = "backup_exporter_velero_oldest_backup_age"
	metricVeleroMaxInterval  = "backup_exporter_velero_backup_max_interval"
	metricVeleroError        = "backup_exporter_velero_error"
)

// Label names used across the metric families.
const (
	labelNamespace    = "namespace"
	labelClusterName  = "cluster_name"
	labelBackup       = "backup"
	labelType         = "type"
	labelResourceName = "resource_name"
	labelResourceType = "resource_type"

	backupLogical = "logical"
	backupWAL     = "wal"
)

const (
	healthUnknown healthState = iota
	healthOK
	healthDegraded
)

// Parse decodes a /backups payload and groups it into PostgreSQL and
// Velero rows plus the global Velero exporter-error note.
func Parse(raw []byte) (*Report, error) {
	var resp BackupsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed decoding /backups response: %w", err)
	}

	idx := indexMetrics(&resp)

	return &Report{
		GeneratedAt:  resp.GeneratedAt,
		Postgres:     groupPostgres(idx),
		Velero:       groupVelero(idx),
		VeleroErrors: groupVeleroErrors(idx),
	}, nil
}

// indexMetrics collapses the families into a name -> samples lookup.
func indexMetrics(resp *BackupsResponse) map[string][]MetricSample {
	idx := make(map[string][]MetricSample, len(resp.Metrics))
	for _, family := range resp.Metrics {
		idx[family.Name] = append(idx[family.Name], family.Samples...)
	}
	return idx
}

// groupPostgres builds one row per (namespace, cluster_name).
func groupPostgres(idx map[string][]MetricSample) []PostgresRow {
	type key struct{ namespace, cluster string }

	seen := map[key]bool{}
	order := []key{}

	families := []string{
		metricLogicalMaxInterval, metricLogicalLatestAge, metricLogicalOldestAge,
		metricWALMaxInterval, metricWALLatestAge, metricWALOldestAge,
		metricPostgresError,
	}
	for _, name := range families {
		for _, sample := range idx[name] {
			k := key{sample.Labels[labelNamespace], sample.Labels[labelClusterName]}
			if !seen[k] {
				seen[k] = true
				order = append(order, k)
			}
		}
	}

	rows := make([]PostgresRow, 0, len(order))
	for _, k := range order {
		base := map[string]string{labelNamespace: k.namespace, labelClusterName: k.cluster}
		rows = append(rows, PostgresRow{
			Namespace:   k.namespace,
			ClusterName: k.cluster,
			Logical: backupColumn{
				Last:   lookupAge(idx[metricLogicalLatestAge], base),
				Oldest: lookupAge(idx[metricLogicalOldestAge], base),
				MaxGap: lookupAge(idx[metricLogicalMaxInterval], base),
				Status: lookupStatus(idx[metricPostgresError], withLabel(base, labelBackup, backupLogical)),
			},
			WAL: backupColumn{
				Last:   lookupAge(idx[metricWALLatestAge], base),
				Oldest: lookupAge(idx[metricWALOldestAge], base),
				MaxGap: lookupAge(idx[metricWALMaxInterval], base),
				Status: lookupStatus(idx[metricPostgresError], withLabel(base, labelBackup, backupWAL)),
			},
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].ClusterName < rows[j].ClusterName
	})
	return rows
}

// groupVelero builds one row per (namespace, resource_name,
// resource_type, backup). The max-interval family carries no "backup"
// label, so it is matched on the resource triple only.
func groupVelero(idx map[string][]MetricSample) []VeleroRow {
	type key struct{ namespace, resource, resourceType, method string }

	seen := map[key]bool{}
	order := []key{}

	for _, name := range []string{metricVeleroLatestAge, metricVeleroOldestAge} {
		for _, sample := range idx[name] {
			k := key{
				sample.Labels[labelNamespace],
				sample.Labels[labelResourceName],
				sample.Labels[labelResourceType],
				sample.Labels[labelBackup],
			}
			if !seen[k] {
				seen[k] = true
				order = append(order, k)
			}
		}
	}

	rows := make([]VeleroRow, 0, len(order))
	for _, k := range order {
		base := map[string]string{
			labelNamespace:    k.namespace,
			labelResourceName: k.resource,
			labelResourceType: k.resourceType,
			labelBackup:       k.method,
		}
		gap := map[string]string{
			labelNamespace:    k.namespace,
			labelResourceName: k.resource,
			labelResourceType: k.resourceType,
		}
		last := lookupAge(idx[metricVeleroLatestAge], base)
		maxGap := lookupAge(idx[metricVeleroMaxInterval], gap)
		rows = append(rows, VeleroRow{
			Namespace:    k.namespace,
			ResourceName: k.resource,
			ResourceType: k.resourceType,
			Method:       k.method,
			Last:         last,
			Oldest:       lookupAge(idx[metricVeleroOldestAge], base),
			MaxGap:       maxGap,
			Status:       veleroStatus(last, maxGap),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return veleroSortKey(rows[i]) < veleroSortKey(rows[j])
	})
	return rows
}

// groupVeleroErrors returns the "type" values whose global
// backup_exporter_velero_error gauge is currently non-zero.
func groupVeleroErrors(idx map[string][]MetricSample) []string {
	var inError []string
	for _, sample := range idx[metricVeleroError] {
		if sample.Value != 0 {
			inError = append(inError, sample.Labels[labelType])
		}
	}
	sort.Strings(inError)
	return inError
}

// veleroSortKey yields a stable ordering key for a Velero row.
func veleroSortKey(r VeleroRow) string {
	return r.Namespace + "\x00" + r.ResourceName + "\x00" + r.ResourceType + "\x00" + r.Method
}

// lookupAge returns the first sample whose labels match want, as an
// ageCell. A missing series yields Present == false.
func lookupAge(samples []MetricSample, want map[string]string) ageCell {
	for _, sample := range samples {
		if labelsMatch(sample.Labels, want) {
			return ageCell{Seconds: sample.Value, Present: true}
		}
	}
	return ageCell{Seconds: 0, Present: false}
}

// lookupStatus maps the error gauge (0 ok, non-zero error) matching want
// to a healthState; a missing series is healthUnknown.
func lookupStatus(samples []MetricSample, want map[string]string) healthState {
	for _, sample := range samples {
		if labelsMatch(sample.Labels, want) {
			if sample.Value == 0 {
				return healthOK
			}
			return healthDegraded
		}
	}
	return healthUnknown
}

// labelsMatch reports whether have contains every key/value in want.
func labelsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// withLabel returns a copy of base with k=v added.
func withLabel(base map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for bk, bv := range base {
		out[bk] = bv
	}
	out[k] = v
	return out
}

// veleroStatus derives a row's health from freshness: a latest-backup age
// exceeding the configured max interval is overdue (degraded); a present
// age within interval is OK; a missing latest age is unknown.
func veleroStatus(last, maxGap ageCell) healthState {
	if !last.Present {
		return healthUnknown
	}
	if maxGap.Present && last.Seconds > maxGap.Seconds {
		return healthDegraded
	}
	return healthOK
}

// Overall rolls a PostgreSQL row up: degraded if either present method is
// in error, OK if at least one method is present and none is in error,
// unknown if neither method reported an error gauge.
func (r PostgresRow) Overall() healthState {
	return combineStatus(r.Logical.Status, r.WAL.Status)
}

func combineStatus(a, b healthState) healthState {
	anyOK := false
	for _, s := range []healthState{a, b} {
		switch s {
		case healthDegraded:
			return healthDegraded
		case healthOK:
			anyOK = true
		case healthUnknown:
		}
	}
	if anyOK {
		return healthOK
	}
	return healthUnknown
}
