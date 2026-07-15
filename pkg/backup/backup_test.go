// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixtureGeneratedAt = "2026-07-15T11:26:03Z"
	clusterDemoDB      = "demo-db"
	clusterDemoDB2     = "demo-db-2"
	resourceDemoApp    = "demo-app"
	resourceDemoVol    = "demo-vol"
)

// fixtureBackups is a canned /backups payload exercising both healthy and
// degraded PostgreSQL and Velero series, an omitted WAL family (missing
// series), and a global Velero exporter error.
const fixtureBackups = `{
  "generated_at": "2026-07-15T11:26:03Z",
  "metrics": [
    {
      "name": "postgres_latest_logical_backup_age",
      "help": "seconds since the latest logical backup",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 3661 },
        { "labels": { "namespace": "demo", "cluster_name": "demo-db-2" }, "value": 100000 }
      ]
    },
    {
      "name": "postgres_oldest_logical_backup_age",
      "help": "seconds since the oldest logical backup",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 273600 }
      ]
    },
    {
      "name": "postgres_logical_backup_max_interval",
      "help": "max allowed gap between logical backups",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 86400 }
      ]
    },
    {
      "name": "postgres_latest_cnpg_wal_backup_age",
      "help": "seconds since the latest WAL archive",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 61 }
      ]
    },
    {
      "name": "postgres_oldest_cnpg_wal_backup_age",
      "help": "seconds since the oldest WAL archive",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 3661 }
      ]
    },
    {
      "name": "cnpg_wal_backup_max_interval",
      "help": "max allowed gap between WAL archives",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "cluster_name": "demo-db" }, "value": 300 }
      ]
    },
    {
      "name": "backup_exporter_postgres_error",
      "help": "1 if the last backup errored",
      "type": "gauge",
      "samples": [
        { "labels": { "backup": "logical", "namespace": "demo", "cluster_name": "demo-db", "type": "cnpg" }, "value": 0 },
        { "labels": { "backup": "wal", "namespace": "demo", "cluster_name": "demo-db", "type": "cnpg" }, "value": 0 },
        { "labels": { "backup": "logical", "namespace": "demo", "cluster_name": "demo-db-2", "type": "cnpg" }, "value": 1 },
        { "labels": { "backup": "wal", "namespace": "demo", "cluster_name": "demo-db-2", "type": "cnpg" }, "value": 0 }
      ]
    },
    {
      "name": "backup_exporter_velero_latest_backup_age",
      "help": "seconds since the latest velero backup",
      "type": "gauge",
      "samples": [
        { "labels": { "backup": "restic", "namespace": "demo", "resource_name": "demo-app", "resource_type": "namespaces" }, "value": 3600 },
        { "labels": { "backup": "restic", "namespace": "demo", "resource_name": "demo-vol", "resource_type": "persistentvolumes" }, "value": 200000 }
      ]
    },
    {
      "name": "backup_exporter_velero_oldest_backup_age",
      "help": "seconds since the oldest velero backup",
      "type": "gauge",
      "samples": [
        { "labels": { "backup": "restic", "namespace": "demo", "resource_name": "demo-app", "resource_type": "namespaces" }, "value": 7200 }
      ]
    },
    {
      "name": "backup_exporter_velero_backup_max_interval",
      "help": "max allowed gap between velero backups",
      "type": "gauge",
      "samples": [
        { "labels": { "namespace": "demo", "resource_name": "demo-app", "resource_type": "namespaces" }, "value": 86400 },
        { "labels": { "namespace": "demo", "resource_name": "demo-vol", "resource_type": "persistentvolumes" }, "value": 86400 }
      ]
    },
    {
      "name": "backup_exporter_velero_error",
      "help": "1 if the velero backup errored",
      "type": "gauge",
      "samples": [
        { "labels": { "type": "aws" }, "value": 0 },
        { "labels": { "type": "azure" }, "value": 1 }
      ]
    }
  ]
}`

func findPostgres(t *testing.T, rows []PostgresRow, cluster string) PostgresRow {
	t.Helper()
	for _, row := range rows {
		if row.ClusterName == cluster {
			return row
		}
	}
	t.Fatalf("no PostgreSQL row for cluster %q", cluster)
	return PostgresRow{} //nolint:exhaustruct // unreachable: Fatalf stops the test.
}

func findVelero(t *testing.T, rows []VeleroRow, resource string) VeleroRow {
	t.Helper()
	for _, row := range rows {
		if row.ResourceName == resource {
			return row
		}
	}
	t.Fatalf("no Velero row for resource %q", resource)
	return VeleroRow{} //nolint:exhaustruct // unreachable: Fatalf stops the test.
}

func TestHumanizeSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seconds float64
		want    string
	}{
		{name: "zero", seconds: 0, want: "0s ago"},
		{name: "negative clamps to zero", seconds: -5, want: "0s ago"},
		{name: "seconds only", seconds: 42, want: "42s ago"},
		{name: "minutes and seconds", seconds: 61, want: "1m 1s ago"},
		{name: "whole minutes drop trailing zero", seconds: 300, want: "5m ago"},
		{name: "whole hours drop trailing zero", seconds: 3600, want: "1h ago"},
		{name: "hours and minutes", seconds: 3661, want: "1h 1m ago"},
		{name: "days and hours", seconds: 90061, want: "1d 1h ago"},
		{name: "multi day", seconds: 273600, want: "3d 4h ago"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, humanizeSeconds(tc.seconds))
		})
	}
}

func TestHumanizeAgeMissing(t *testing.T) {
	t.Parallel()
	assert.Equal(t, placeholderMissing, humanizeAge(ageCell{Seconds: 0, Present: false}))
	assert.Equal(t, "1h ago", humanizeAge(ageCell{Seconds: 3600, Present: true}))
}

func TestParseGroupsPostgres(t *testing.T) {
	t.Parallel()

	report, err := Parse([]byte(fixtureBackups))
	require.NoError(t, err)
	assert.Equal(t, fixtureGeneratedAt, report.GeneratedAt)
	require.Len(t, report.Postgres, 2)

	healthy := findPostgres(t, report.Postgres, clusterDemoDB)
	assert.Equal(t, "demo", healthy.Namespace)
	assert.Equal(t, healthOK, healthy.Logical.Status)
	assert.Equal(t, healthOK, healthy.WAL.Status)
	assert.Equal(t, healthOK, healthy.Overall())
	assert.Equal(t, "1h 1m ago", humanizeAge(healthy.Logical.Last))
	assert.Equal(t, "3d 4h ago", humanizeAge(healthy.Logical.Oldest))
	assert.Equal(t, "1m 1s ago", humanizeAge(healthy.WAL.Last))

	degraded := findPostgres(t, report.Postgres, clusterDemoDB2)
	assert.Equal(t, healthDegraded, degraded.Logical.Status)
	assert.Equal(t, healthOK, degraded.WAL.Status)
	assert.Equal(t, healthDegraded, degraded.Overall())
	// WAL age families were omitted for demo-db-2: missing series.
	assert.False(t, degraded.WAL.Last.Present)
	assert.Equal(t, placeholderMissing, humanizeAge(degraded.WAL.Last))
}

func TestParseGroupsVelero(t *testing.T) {
	t.Parallel()

	report, err := Parse([]byte(fixtureBackups))
	require.NoError(t, err)
	require.Len(t, report.Velero, 2)

	fresh := findVelero(t, report.Velero, resourceDemoApp)
	assert.Equal(t, "restic", fresh.Method)
	assert.Equal(t, "namespaces", fresh.ResourceType)
	assert.Equal(t, healthOK, fresh.Status)
	assert.Equal(t, "1h ago", humanizeAge(fresh.Last))
	assert.Equal(t, "2h ago", humanizeAge(fresh.Oldest))
	assert.Equal(t, "1d ago", humanizeAge(fresh.MaxGap))

	// Latest age (200000s) exceeds the max interval (86400s): overdue.
	overdue := findVelero(t, report.Velero, resourceDemoVol)
	assert.Equal(t, healthDegraded, overdue.Status)
	assert.Equal(t, "2d 7h ago", humanizeAge(overdue.Last))
	// oldest age family omitted for this resource: missing series.
	assert.Equal(t, placeholderMissing, humanizeAge(overdue.Oldest))

	assert.Equal(t, []string{"azure"}, report.VeleroErrors)
}

func TestParseInvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte("not-json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed decoding")
}

func TestVeleroStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		last   ageCell
		maxGap ageCell
		want   healthState
	}{
		{
			name:   "missing latest age is unknown",
			last:   ageCell{Seconds: 0, Present: false},
			maxGap: ageCell{Seconds: 100, Present: true},
			want:   healthUnknown,
		},
		{
			name:   "within interval is ok",
			last:   ageCell{Seconds: 50, Present: true},
			maxGap: ageCell{Seconds: 100, Present: true},
			want:   healthOK,
		},
		{
			name:   "exceeding interval is degraded",
			last:   ageCell{Seconds: 200, Present: true},
			maxGap: ageCell{Seconds: 100, Present: true},
			want:   healthDegraded,
		},
		{
			name:   "present age with no interval is ok",
			last:   ageCell{Seconds: 999, Present: true},
			maxGap: ageCell{Seconds: 0, Present: false},
			want:   healthOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, veleroStatus(tc.last, tc.maxGap))
		})
	}
}

// TestStatus exercises the fetch seam end to end. It is not parallel:
// each case reassigns the package-level fetchBackups stub.
func TestStatus(t *testing.T) {
	origFetch := fetchBackups
	t.Cleanup(func() { fetchBackups = origFetch })

	fixtureStub := func(_ context.Context, _, _, _, _, _ string) ([]byte, error) {
		return []byte(fixtureBackups), nil
	}
	errStub := func(_ context.Context, _, _, _, _, _ string) ([]byte, error) {
		return nil, errors.New("boom")
	}

	tests := []struct {
		name          string
		stub          func(context.Context, string, string, string, string, string) ([]byte, error)
		output        OutputFormat
		wantErrSubstr string
		wantContains  []string
		wantExcludes  []string
	}{
		{
			name:         "json passthrough is unmodified",
			stub:         fixtureStub,
			output:       OutputJSON,
			wantContains: []string{`"generated_at"`, `"backup_exporter_velero_error"`},
			wantExcludes: []string{"PostgreSQL backups", "data as of"},
		},
		{
			name:         "table renders sections and status",
			stub:         fixtureStub,
			output:       OutputTable,
			wantContains: []string{"PostgreSQL backups", "Velero backups", "DEGRADED", "data as of " + fixtureGeneratedAt},
		},
		{
			name:         "wide expands columns",
			stub:         fixtureStub,
			output:       OutputWide,
			wantContains: []string{"LOGICAL LAST", "WAL MAX GAP"},
		},
		{
			name:          "fetch error propagates",
			stub:          errStub,
			output:        OutputTable,
			wantErrSubstr: "boom",
		},
		{
			name:          "invalid output format rejected",
			stub:          fixtureStub,
			output:        OutputFormat("yaml"),
			wantErrSubstr: "invalid output format",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fetchBackups = tc.stub

			var buf bytes.Buffer
			err := Status(context.Background(), Options{
				Kubeconfig: "",
				Context:    "",
				Namespace:  DefaultNamespace,
				Service:    DefaultService,
				Port:       DefaultPort,
				Output:     tc.output,
				Out:        &buf,
			})

			if tc.wantErrSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			out := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, out, want)
			}
			for _, exclude := range tc.wantExcludes {
				assert.NotContains(t, out, exclude)
			}
		})
	}
}

func TestStatusJSONExactPassthrough(t *testing.T) {
	origFetch := fetchBackups
	t.Cleanup(func() { fetchBackups = origFetch })

	raw := []byte(fixtureBackups)
	fetchBackups = func(_ context.Context, _, _, _, _, _ string) ([]byte, error) {
		return raw, nil
	}

	var buf bytes.Buffer
	err := Status(context.Background(), Options{
		Kubeconfig: "",
		Context:    "",
		Namespace:  DefaultNamespace,
		Service:    DefaultService,
		Port:       DefaultPort,
		Output:     OutputJSON,
		Out:        &buf,
	})
	require.NoError(t, err)

	// The payload is written verbatim, with at most a single appended
	// trailing newline for terminal cleanliness.
	got := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	assert.Equal(t, raw, got)
}
