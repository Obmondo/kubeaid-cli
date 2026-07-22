// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func floatPtr(f float64) *float64                { return &f }
func timePtr(t time.Time) *time.Time             { return &t }
func durationPtr(d time.Duration) *time.Duration { return &d }

func TestAdjustedAge(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	collectedAt := now.Add(-10 * time.Minute)

	testCases := []struct {
		name        string
		ageSeconds  *float64
		collectedAt *time.Time
		expected    *time.Duration
	}{
		{
			name:        "nil age seconds returns nil (no series published)",
			ageSeconds:  nil,
			collectedAt: timePtr(collectedAt),
			expected:    nil,
		},
		{
			name:        "nil collected_at returns nil (no reference point to adjust from)",
			ageSeconds:  floatPtr(3600),
			collectedAt: nil,
			expected:    nil,
		},
		{
			name:        "normal case adds elapsed time since collection",
			ageSeconds:  floatPtr(3600),       // 1h old as of collection
			collectedAt: timePtr(collectedAt), // collected 10m ago
			expected:    durationPtr(70 * time.Minute),
		},
		{
			name:        "zero age (no-backup sentinel) still gets adjusted",
			ageSeconds:  floatPtr(0),
			collectedAt: timePtr(collectedAt),
			expected:    durationPtr(10 * time.Minute),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := adjustedAge(tc.ageSeconds, tc.collectedAt, now)
			if tc.expected == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.expected, *got)
		})
	}
}

func TestFormatLatestAge(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	collectedAt := now.Add(-10 * time.Minute)

	testCases := []struct {
		name        string
		resource    backupResource
		collectedAt *time.Time
		expected    string
	}{
		{
			name:        "nil age renders as -",
			resource:    backupResource{LatestBackupAgeSeconds: nil},
			collectedAt: timePtr(collectedAt),
			expected:    "-",
		},
		{
			name:        "zero age renders as none (no_backup sentinel)",
			resource:    backupResource{LatestBackupAgeSeconds: floatPtr(0)},
			collectedAt: timePtr(collectedAt),
			expected:    "none",
		},
		{
			name:        "normal age is humanized and adjusted for elapsed collection time",
			resource:    backupResource{LatestBackupAgeSeconds: floatPtr(3 * 3600)},
			collectedAt: timePtr(collectedAt),
			expected:    "3h 10m", // 3h at collection + 10m elapsed since
		},
		{
			name:        "non-nil age but nil collected_at renders as - (can't safely adjust)",
			resource:    backupResource{LatestBackupAgeSeconds: floatPtr(3600)},
			collectedAt: nil,
			expected:    "-",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatLatestAge(tc.resource, tc.collectedAt, now))
		})
	}
}

func TestFormatAge(t *testing.T) {
	testCases := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{name: "sub-minute keeps seconds", duration: 45 * time.Second, expected: "45s"},
		{name: "negative clamps to zero", duration: -3 * time.Second, expected: "0s"},
		{name: "a minute drops seconds", duration: 90 * time.Second, expected: "1m"},
		{name: "under an hour is minutes", duration: 45 * time.Minute, expected: "45m"},
		{name: "the case kubectl renders as 177m", duration: 177 * time.Minute, expected: "2h 57m"},
		{name: "a whole hour drops the minutes", duration: time.Hour, expected: "1h"},
		{name: "13h stays 13h", duration: 13 * time.Hour, expected: "13h"},
		{name: "just under a day", duration: 23*time.Hour + 59*time.Minute, expected: "23h 59m"},
		{name: "a day drops the hours", duration: 24 * time.Hour, expected: "1d"},
		{name: "days carry hours, never minutes", duration: 3*24*time.Hour + 4*time.Hour + 30*time.Minute, expected: "3d 4h"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatAge(tc.duration))
		})
	}
}

// TestFormatCollectorLine covers contract subtlety: a collector that has never completed a
// run (nil CollectedAt) must be surfaced explicitly, not rendered as a stale or empty age.
func TestFormatCollectorHeader(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	testCases := []struct {
		name       string
		collectors []backupCollector
		expected   string
	}{
		{
			name:       "no collectors reported",
			collectors: nil,
			expected:   "no collectors reported",
		},
		{
			name: "single collector",
			collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: timePtr(now.Add(-4 * time.Minute))},
			},
			expected: "collected 4m ago: cnpg",
		},
		{
			name: "collectors sharing an age condense to one age",
			collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: timePtr(now.Add(-87 * time.Minute))},
				{Operator: "velero", CollectedAt: timePtr(now.Add(-87 * time.Minute))},
			},
			expected: "collected 1h 27m ago: cnpg | velero",
		},
		{
			name: "differing ages are reported per operator",
			collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: timePtr(now.Add(-87 * time.Minute))},
				{Operator: "velero", CollectedAt: timePtr(now.Add(-5 * time.Minute))},
			},
			expected: "collected: cnpg 1h 27m ago | velero 5m ago",
		},
		{
			name: "a collector that never ran is named beside one that has",
			collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: timePtr(now.Add(-5 * time.Minute))},
				{Operator: "velero", CollectedAt: nil},
			},
			expected: "collected: cnpg 5m ago | velero never",
		},
		{
			name: "no collector has ever completed a run",
			collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: nil},
				{Operator: "velero", CollectedAt: nil},
			},
			expected: "no completed collection run yet: cnpg | velero",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatCollectorHeader(tc.collectors, now))
		})
	}
}

func TestCountByStatus(t *testing.T) {
	t.Run("each resource counts once, under its worst stream status", func(t *testing.T) {
		resources := []backupResource{
			// A missing logical backup and a failed wal check on one cluster is one resource
			// in trouble, not one no_backup plus one collector_error.
			{Namespace: "demo", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "logical", Status: backupStatusNoBackup},
			{Namespace: "demo", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "wal", Status: backupStatusCollectorError},
			{Namespace: "demo", ResourceName: "uploads", ResourceType: "pvc", Stream: "volume", Status: backupStatusHealthy},
			{Namespace: "other", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "logical", Status: backupStatusExceedsRPO},
		}

		counts, total := countByStatus(resources)

		assert.Equal(t, 3, total)
		assert.Equal(t, map[string]int{
			backupStatusNoBackup:   1,
			backupStatusHealthy:    1,
			backupStatusExceedsRPO: 1,
		}, counts)
	})

	t.Run("a healthy stream never masks a failing one", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "demo", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "wal", Status: backupStatusHealthy},
			{Namespace: "demo", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "logical", Status: backupStatusNoBackup},
		}

		counts, total := countByStatus(resources)

		assert.Equal(t, 1, total)
		assert.Equal(t, map[string]int{backupStatusNoBackup: 1}, counts)
	})

	t.Run("resources sharing a name in different namespaces stay separate", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "demo-a", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "logical", Status: backupStatusHealthy},
			{Namespace: "demo-b", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "logical", Status: backupStatusHealthy},
		}

		counts, total := countByStatus(resources)

		assert.Equal(t, 2, total)
		assert.Equal(t, map[string]int{backupStatusHealthy: 2}, counts)
	})

	t.Run("an unrecognized status outranks every known one", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "demo", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "logical", Status: backupStatusHealthy},
			{Namespace: "demo", ResourceName: "pgsql", ResourceType: "cnpg_cluster", Stream: "wal", Status: "some_new_status"},
		}

		counts, _ := countByStatus(resources)

		assert.Equal(t, map[string]int{"some_new_status": 1}, counts)
	})
}

func TestFormatStatusSummary(t *testing.T) {
	testCases := []struct {
		name     string
		counts   map[string]int
		total    int
		expected string
	}{
		{
			name:     "no resources",
			counts:   map[string]int{},
			expected: "no resources reported",
		},
		{
			name:     "a single resource reads singular",
			counts:   map[string]int{backupStatusHealthy: 1},
			total:    1,
			expected: "1 resource: 1 healthy",
		},
		{
			name: "known statuses render in canonical order regardless of map order",
			counts: map[string]int{
				backupStatusNoBackup:   2,
				backupStatusHealthy:    12,
				backupStatusExceedsRPO: 1,
			},
			total:    15,
			expected: "15 resources: 12 healthy, 1 exceeds_rpo, 2 no_backup",
		},
		{
			name: "all five known statuses",
			counts: map[string]int{
				backupStatusHealthy:        1,
				backupStatusExceedsRPO:     1,
				backupStatusNoBackup:       1,
				backupStatusCollectorError: 1,
				backupStatusUnknown:        1,
			},
			total:    5,
			expected: "5 resources: 1 healthy, 1 exceeds_rpo, 1 no_backup, 1 collector_error, 1 unknown",
		},
		{
			name: "unrecognized status is appended sorted, not dropped",
			counts: map[string]int{
				backupStatusHealthy: 1,
				"weird_status":      1,
				"another_status":    1,
			},
			total:    3,
			expected: "3 resources: 1 healthy, 1 another_status, 1 weird_status",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatStatusSummary(tc.counts, tc.total))
		})
	}
}

// TestBackupResponseJSONDecoding decodes a representative GET /api/v1/backups fixture,
// exercising the contract subtleties a naive decode would miss: a collector that has never
// run (null collected_at), the no-backup sentinel (age exactly 0, distinct from a genuinely
// absent age), a collector_error resource with its error_type, and an operator_errors entry.
func TestBackupResponseJSONDecoding(t *testing.T) {
	const fixture = `{
  "collectors": [
    { "operator": "cnpg", "collected_at": "2026-07-16T08:00:00Z" },
    { "operator": "velero", "collected_at": null }
  ],
  "resources": [
    {
      "operator": "cnpg",
      "stream": "logical",
      "namespace": "demo",
      "resource_name": "demo-db",
      "resource_type": "cnpg_cluster",
      "latest_backup_age_seconds": 3600,
      "oldest_backup_age_seconds": 604800,
      "max_interval_seconds": 601200,
      "status": "healthy"
    },
    {
      "operator": "velero",
      "stream": "volume",
      "namespace": "demo",
      "resource_name": "demo-app",
      "resource_type": "pvc",
      "method": "CSISnapshot",
      "latest_backup_age_seconds": 0,
      "oldest_backup_age_seconds": null,
      "max_interval_seconds": null,
      "status": "no_backup"
    },
    {
      "operator": "cnpg",
      "stream": "wal",
      "namespace": "demo",
      "resource_name": "demo-db",
      "resource_type": "cnpg_cluster",
      "latest_backup_age_seconds": null,
      "oldest_backup_age_seconds": null,
      "max_interval_seconds": null,
      "status": "collector_error",
      "error_type": "archive_command_failed"
    }
  ],
  "operator_errors": [
    { "operator": "velero", "type": "s3_list_failed" }
  ]
}`

	var response backupResponse
	err := json.Unmarshal([]byte(fixture), &response)
	require.NoError(t, err)

	require.Len(t, response.Collectors, 2)
	assert.Equal(t, "cnpg", response.Collectors[0].Operator)
	require.NotNil(t, response.Collectors[0].CollectedAt)
	assert.True(t, response.Collectors[0].CollectedAt.Equal(time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)))
	assert.Equal(t, "velero", response.Collectors[1].Operator)
	assert.Nil(t, response.Collectors[1].CollectedAt, "an operator that never ran must decode to a nil timestamp, not be omitted")

	require.Len(t, response.Resources, 3)

	healthy := response.Resources[0]
	require.NotNil(t, healthy.LatestBackupAgeSeconds)
	assert.InDelta(t, 3600, *healthy.LatestBackupAgeSeconds, 0)
	assert.Equal(t, backupStatusHealthy, healthy.Status)

	noBackup := response.Resources[1]
	require.NotNil(t, noBackup.LatestBackupAgeSeconds, "a reported zero age must decode as a pointer to 0, not nil")
	assert.InDelta(t, 0, *noBackup.LatestBackupAgeSeconds, 0)
	assert.Nil(t, noBackup.OldestBackupAgeSeconds, "a genuinely absent age must decode as nil, not 0")
	assert.Equal(t, "CSISnapshot", noBackup.Method)

	collectorErr := response.Resources[2]
	assert.Nil(t, collectorErr.LatestBackupAgeSeconds)
	assert.Equal(t, backupStatusCollectorError, collectorErr.Status)
	assert.Equal(t, "archive_command_failed", collectorErr.ErrorType)

	require.Len(t, response.OperatorErrors, 1)
	assert.Equal(t, "velero", response.OperatorErrors[0].Operator)
	assert.Equal(t, "s3_list_failed", response.OperatorErrors[0].Type)
}

// TestFormatStatus covers the easy-to-regress edge case: error_type is omitempty on the wire,
// so a collector_error resource routinely decodes with an empty ErrorType, and the STATUS cell
// must fall through to the bare status rather than leaving a dangling " ()".
func TestFormatMethod(t *testing.T) {
	testCases := []struct {
		name     string
		resource backupResource
		expected string
	}{
		{
			name:     "Velero's own name is carried through verbatim",
			resource: backupResource{Operator: "velero", Stream: "volume", Method: "PodVolumeBackup"},
			expected: "PodVolumeBackup",
		},
		{
			name:     "acronyms are not reshaped",
			resource: backupResource{Operator: "velero", Stream: "volume", Method: "CSISnapshot"},
			expected: "CSISnapshot",
		},
		{
			name:     "CNPG's logical backup is taken by a CronJob",
			resource: backupResource{Operator: "cnpg", Stream: "logical"},
			expected: "CronJob",
		},
		{
			name:     "CNPG's wal archive is taken by barman-cloud",
			resource: backupResource{Operator: "cnpg", Stream: "wal"},
			expected: "Barman",
		},
		{
			name:     "a reported method wins over the derived one",
			resource: backupResource{Operator: "cnpg", Stream: "logical", Method: "SomethingElse"},
			expected: "SomethingElse",
		},
		{
			name:     "an unrecognized CNPG stream has no method to name",
			resource: backupResource{Operator: "cnpg", Stream: "something_new"},
			expected: "-",
		},
		{
			name:     "another operator's stream is not given a CNPG mechanism",
			resource: backupResource{Operator: "mongodb", Stream: "logical"},
			expected: "-",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatMethod(tc.resource))
		})
	}
}

func TestFormatStatus(t *testing.T) {
	testCases := []struct {
		name     string
		resource backupResource
		expected string
	}{
		{
			name:     "plain status passes through unchanged",
			resource: backupResource{Status: backupStatusHealthy},
			expected: "healthy",
		},
		{
			name:     "collector_error with a non-empty error_type folds it into the cell",
			resource: backupResource{Status: backupStatusCollectorError, ErrorType: "archive_command_failed"},
			expected: "collector_error (archive_command_failed)",
		},
		{
			name:     "collector_error with an empty error_type falls through to the bare status",
			resource: backupResource{Status: backupStatusCollectorError, ErrorType: ""},
			expected: "collector_error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, formatStatus(tc.resource))
		})
	}
}

// TestSortResources exercises the full 5-key cascading comparator documented on sortResources
// as "the display contract". Each pair of rows below ties on every key up to (but not
// including) the one it's meant to test, so the first two keys (Namespace, ResourceName) alone
// can never produce the asserted order by accident - only the deeper key can. ResourceType
// carries a "tiebreak" tag identifying each row post-sort; sortResources never reads that
// field, so it can't influence the outcome it's used to verify.
func TestSortResources(t *testing.T) {
	resources := []backupResource{
		// Ties on Namespace+ResourceName+Operator+Stream; only Method differs.
		{Namespace: "backup", ResourceName: "app-c", Operator: "velero", Stream: "volume", Method: "Restic", ResourceType: "method-tiebreak-2"},
		{Namespace: "backup", ResourceName: "app-c", Operator: "velero", Stream: "volume", Method: "CSISnapshot", ResourceType: "method-tiebreak-1"},

		// Ties on Namespace+ResourceName+Operator; only Stream differs.
		{Namespace: "backup", ResourceName: "app-b", Operator: "cnpg", Stream: "wal", ResourceType: "stream-tiebreak-2"},
		{Namespace: "backup", ResourceName: "app-b", Operator: "cnpg", Stream: "logical", ResourceType: "stream-tiebreak-1"},

		// Ties on Namespace+ResourceName; only Operator differs.
		{Namespace: "backup", ResourceName: "app-a", Operator: "velero", ResourceType: "operator-tiebreak-2"},
		{Namespace: "backup", ResourceName: "app-a", Operator: "cnpg", ResourceType: "operator-tiebreak-1"},
	}

	sortResources(resources)

	got := make([]string, len(resources))
	for i, r := range resources {
		got[i] = r.ResourceType
	}

	assert.Equal(t, []string{
		"operator-tiebreak-1", // app-a/cnpg before app-a/velero
		"operator-tiebreak-2",
		"stream-tiebreak-1", // app-b/cnpg/logical before app-b/cnpg/wal
		"stream-tiebreak-2",
		"method-tiebreak-1", // app-c/velero/volume/CSISnapshot before .../Restic
		"method-tiebreak-2",
	}, got)
}

func TestAnyMethod(t *testing.T) {
	testCases := []struct {
		name      string
		resources []backupResource
		expected  bool
	}{
		{
			name:      "empty slice",
			resources: []backupResource{},
			expected:  false,
		},
		{
			name: "all resources have an empty Method and no mechanism to derive",
			resources: []backupResource{
				{Namespace: "backup", ResourceName: "app-a"},
				{Namespace: "backup", ResourceName: "app-b"},
			},
			expected: false,
		},
		{
			name: "a CNPG row's mechanism counts, though it reports no Method",
			resources: []backupResource{
				{Namespace: "backup", ResourceName: "app-a", Operator: "cnpg", Stream: "wal"},
			},
			expected: true,
		},
		{
			name: "one resource has a non-empty Method",
			resources: []backupResource{
				{Namespace: "backup", ResourceName: "app-a"},
				{Namespace: "backup", ResourceName: "app-b", Method: "CSISnapshot"},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, anyMethod(tc.resources))
		})
	}
}

// TestRenderResourceTable intentionally uses substring assertions rather than an exact-string
// or golden-file match: tabwriter's column padding makes exact matches brittle, and no other
// table-render function in this repo has an exact-match test either.
func TestRenderResourceTable(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	t.Run("includes the METHOD header when a resource carries a method", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "backup", ResourceName: "app-a", Operator: "velero", Stream: "volume", Method: "CSISnapshot", Status: backupStatusHealthy},
		}
		out := renderResourceTable(resources, map[string]*time.Time{}, now)
		assert.Contains(t, out, "METHOD")
	})

	t.Run("omits the METHOD header when no resource carries a method", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "backup", ResourceName: "app-a", Operator: "mongodb", Stream: "logical", Status: backupStatusHealthy},
		}
		out := renderResourceTable(resources, map[string]*time.Time{}, now)
		assert.NotContains(t, out, "METHOD")
	})

	t.Run("renders resource rows in sortResources order", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "backup", ResourceName: "zzz-last", Operator: "cnpg", Stream: "logical", Status: backupStatusHealthy},
			{Namespace: "backup", ResourceName: "aaa-first", Operator: "cnpg", Stream: "logical", Status: backupStatusHealthy},
			{Namespace: "backup", ResourceName: "mmm-middle", Operator: "cnpg", Stream: "logical", Status: backupStatusHealthy},
		}
		out := renderResourceTable(resources, map[string]*time.Time{}, now)

		firstIdx := strings.Index(out, "aaa-first")
		middleIdx := strings.Index(out, "mmm-middle")
		lastIdx := strings.Index(out, "zzz-last")
		require.NotEqual(t, -1, firstIdx, "aaa-first must appear in the rendered table")
		require.NotEqual(t, -1, middleIdx, "mmm-middle must appear in the rendered table")
		require.NotEqual(t, -1, lastIdx, "zzz-last must appear in the rendered table")

		assert.Less(t, firstIdx, middleIdx, "aaa-first must render before mmm-middle")
		assert.Less(t, middleIdx, lastIdx, "mmm-middle must render before zzz-last")
	})

	t.Run("one row per backup stream, under a single header row", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "demo", ResourceName: "demo-pgsql", ResourceType: "cnpg_cluster", Operator: "cnpg", Stream: "logical", Status: backupStatusNoBackup},
			{Namespace: "demo", ResourceName: "demo-pgsql", ResourceType: "cnpg_cluster", Operator: "cnpg", Stream: "wal", Status: backupStatusHealthy},
			{Namespace: "demo", ResourceName: "demo-uploads", ResourceType: "pvc", Operator: "velero", Stream: "volume", Status: backupStatusHealthy},
		}

		out := renderResourceTable(resources, map[string]*time.Time{}, now)

		lines := strings.Split(out, "\n")
		require.Len(t, lines, len(resources)+1, "one header row plus one row per stream")
		assert.True(t, strings.HasPrefix(lines[0], "NAMESPACE"), "the header row comes first")
	})

	t.Run("columns line up across rows of differing width", func(t *testing.T) {
		resources := []backupResource{
			{Namespace: "demo", ResourceName: "a-much-longer-name", ResourceType: "pvc", Operator: "velero", Stream: "volume", Status: backupStatusHealthy},
			{Namespace: "demo", ResourceName: "short", ResourceType: "pvc", Operator: "velero", Stream: "volume", Status: backupStatusHealthy},
		}

		out := renderResourceTable(resources, map[string]*time.Time{}, now)

		lines := strings.Split(out, "\n")
		require.Len(t, lines, 3)
		assert.Equal(t, strings.Index(lines[1], "pvc"), strings.Index(lines[2], "pvc"),
			"the TYPE column starts at one offset for every row")
	})
}

// TestRenderBackupStatus, like TestRenderResourceTable, sticks to substring/position assertions
// rather than an exact-string match.
func TestRenderBackupStatus(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	t.Run("collector freshness is the first line, in Collectors order", func(t *testing.T) {
		resp := backupResponse{
			Collectors: []backupCollector{
				{Operator: "velero", CollectedAt: timePtr(now.Add(-5 * time.Minute))},
				{Operator: "cnpg", CollectedAt: nil},
			},
			Resources: []backupResource{
				{Namespace: "backup", ResourceName: "app-a", Operator: "velero", Stream: "volume", Status: backupStatusHealthy},
			},
		}

		out := renderBackupStatus(resp, now)

		lines := strings.Split(out, "\n")
		require.NotEmpty(t, lines)
		assert.Equal(t, "collected: velero 5m ago | cnpg never", lines[0])
	})

	t.Run("Operator errors section present when OperatorErrors is non-empty", func(t *testing.T) {
		resp := backupResponse{
			OperatorErrors: []backupOperatorError{
				{Operator: "velero", Type: "s3_list_failed"},
			},
		}

		out := renderBackupStatus(resp, now)

		assert.Contains(t, out, "Operator errors:")
		assert.Contains(t, out, "velero: s3_list_failed")
	})

	t.Run("Operator errors section absent when OperatorErrors is empty", func(t *testing.T) {
		resp := backupResponse{}

		out := renderBackupStatus(resp, now)

		assert.NotContains(t, out, "Operator errors:")
	})

	t.Run("status summary line is the last non-empty line", func(t *testing.T) {
		resp := backupResponse{
			Collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: timePtr(now.Add(-5 * time.Minute))},
			},
			Resources: []backupResource{
				{Namespace: "backup", ResourceName: "app-a", Operator: "cnpg", Stream: "logical", Status: backupStatusHealthy},
			},
		}

		out := renderBackupStatus(resp, now)

		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		require.NotEmpty(t, lines)

		expectedSummary := formatStatusSummary(countByStatus(resp.Resources))
		assert.Equal(t, expectedSummary, lines[len(lines)-1])
	})

	t.Run("no resource table when Resources is empty", func(t *testing.T) {
		// renderBackupStatus skips calling renderResourceTable entirely when
		// len(resp.Resources) == 0, rather than rendering a headers-only table.
		resp := backupResponse{
			Collectors: []backupCollector{
				{Operator: "cnpg", CollectedAt: timePtr(now.Add(-5 * time.Minute))},
			},
		}

		out := renderBackupStatus(resp, now)

		assert.NotContains(t, out, "NAMESPACE", "no table header should render when there are zero resources")
		assert.Contains(t, out, "no resources reported")
	})
}

// backupExporterServiceFixture returns a Service carrying the label backup-exporter's Helm
// chart stamps on its Service, in namespace "monitoring".
func backupExporterServiceFixture() *coreV1.Service {
	return &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "backup-exporter",
			Namespace: "monitoring",
			Labels:    map[string]string{backupExporterLabelKey: backupExporterLabelValue},
		},
	}
}

func TestFindBackupExporterService(t *testing.T) {
	// Seeded into every case below to confirm the label selector actually filters, rather
	// than findBackupExporterService just returning whatever Services happen to exist.
	unlabeledService := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "some-other-service",
			Namespace: "monitoring",
		},
	}

	t.Run("zero matching services returns an error", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(unlabeledService)

		_, err := findBackupExporterService(context.Background(), clientset)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no backup-exporter service found")
	})

	t.Run("exactly one matching service returns it", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(unlabeledService, backupExporterServiceFixture())

		svc, err := findBackupExporterService(context.Background(), clientset)
		require.NoError(t, err)
		assert.Equal(t, "monitoring", svc.Namespace)
		assert.Equal(t, "backup-exporter", svc.Name)
	})

	t.Run("the default namespace wins over a match elsewhere", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(
			unlabeledService,
			backupExporterServiceFixture(),
			labeledServiceIn("backup"),
		)

		svc, err := findBackupExporterService(context.Background(), clientset)
		require.NoError(t, err)
		assert.Equal(t, backupExporterDefaultNamespace, svc.Namespace,
			"a second exporter elsewhere must not make the default-namespace one ambiguous")
	})

	t.Run("a single service outside the default namespace is found by the fallback", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(unlabeledService, labeledServiceIn("backup"))

		svc, err := findBackupExporterService(context.Background(), clientset)
		require.NoError(t, err)
		assert.Equal(t, "backup", svc.Namespace)
	})

	t.Run("the fallback runs when the default namespace is refused", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(labeledServiceIn("backup"))
		clientset.PrependReactor("list", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetNamespace() == backupExporterDefaultNamespace {
				return true, nil, apiErrors.NewForbidden(coreV1.Resource("services"), "", errors.New("no namespaced access"))
			}

			return false, nil, nil
		})

		svc, err := findBackupExporterService(context.Background(), clientset)
		require.NoError(t, err)
		assert.Equal(t, "backup", svc.Namespace)
	})

	t.Run("two matching services, neither in the default namespace, returns an error naming both", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(unlabeledService, labeledServiceIn("backup"), labeledServiceIn("velero"))

		_, err := findBackupExporterService(context.Background(), clientset)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup/backup-exporter")
		assert.Contains(t, err.Error(), "velero/backup-exporter")
	})
}

// labeledServiceIn returns a Service carrying backup-exporter's chart label in namespace.
func labeledServiceIn(namespace string) *coreV1.Service {
	return &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "backup-exporter",
			Namespace: namespace,
			Labels:    map[string]string{backupExporterLabelKey: backupExporterLabelValue},
		},
	}
}

// readyPodFixture returns a Pod carrying podLabels, Running with a Ready=True condition - the
// bar findReadyPod requires - in namespace "monitoring" (matching backupExporterServiceFixture).
// Tests mutate the returned pod's Status to build not-ready variants.
func readyPodFixture(name string, podLabels map[string]string) *coreV1.Pod {
	return &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: "monitoring",
			Labels:    podLabels,
		},
		Status: coreV1.PodStatus{
			Phase: coreV1.PodRunning,
			Conditions: []coreV1.PodCondition{
				{Type: coreV1.PodReady, Status: coreV1.ConditionTrue},
			},
		},
	}
}

// TestFindReadyPod exercises the selector as a real filter (a mismatched-label pod is seeded
// alongside matching ones, same rationale as TestFindBackupExporterService's unlabeledService)
// and the Running+Ready bar itself, using the fake clientset. It never touches
// fetchViaPortForward/newPortForwardDialer: those call clientset.CoreV1().RESTClient(), which
// the fake clientset returns as a typed-nil *rest.RESTClient - calling into it panics, so the
// port-forward stream itself is deliberately left untested here.
func TestFindReadyPod(t *testing.T) {
	selector := map[string]string{backupExporterLabelKey: backupExporterLabelValue}

	t.Run("returns the first Running+Ready pod, skipping not-ready and mismatched-label pods", func(t *testing.T) {
		notReady := readyPodFixture("backup-exporter-not-ready", selector)
		notReady.Status.Conditions[0].Status = coreV1.ConditionFalse

		otherLabels := readyPodFixture("some-other-pod", map[string]string{"app": "unrelated"})

		ready := readyPodFixture("backup-exporter-ready", selector)

		clientset := fake.NewSimpleClientset(notReady, otherLabels, ready)

		pod, err := findReadyPod(context.Background(), clientset, "monitoring", "backup-exporter", selector)
		require.NoError(t, err)
		assert.Equal(t, "backup-exporter-ready", pod.Name)
	})

	t.Run("a matching pod that's never Running is not selected", func(t *testing.T) {
		pending := readyPodFixture("backup-exporter-pending", selector)
		pending.Status.Phase = coreV1.PodPending

		clientset := fake.NewSimpleClientset(pending)

		_, err := findReadyPod(context.Background(), clientset, "monitoring", "backup-exporter", selector)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no ready pod behind service monitoring/backup-exporter")
	})

	t.Run("no pods match the selector errors naming the service, not a generic empty list", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		_, err := findReadyPod(context.Background(), clientset, "monitoring", "backup-exporter", selector)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "monitoring/backup-exporter")
	})

	t.Run("an empty selector errors without listing pods", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		_, err := findReadyPod(context.Background(), clientset, "monitoring", "backup-exporter", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no pod selector")
	})
}

// TestPodReady covers the two-part contract (Running phase AND a Ready=True condition)
// independently: neither one alone is enough.
func TestPodReady(t *testing.T) {
	testCases := []struct {
		name     string
		pod      *coreV1.Pod
		expected bool
	}{
		{
			name: "running and ready",
			pod: &coreV1.Pod{Status: coreV1.PodStatus{
				Phase:      coreV1.PodRunning,
				Conditions: []coreV1.PodCondition{{Type: coreV1.PodReady, Status: coreV1.ConditionTrue}},
			}},
			expected: true,
		},
		{
			name: "running but the Ready condition is False",
			pod: &coreV1.Pod{Status: coreV1.PodStatus{
				Phase:      coreV1.PodRunning,
				Conditions: []coreV1.PodCondition{{Type: coreV1.PodReady, Status: coreV1.ConditionFalse}},
			}},
			expected: false,
		},
		{
			name: "running but no PodReady condition was ever reported",
			pod: &coreV1.Pod{Status: coreV1.PodStatus{
				Phase:      coreV1.PodRunning,
				Conditions: []coreV1.PodCondition{{Type: coreV1.PodInitialized, Status: coreV1.ConditionTrue}},
			}},
			expected: false,
		},
		{
			name: "marked ready but not in the Running phase",
			pod: &coreV1.Pod{Status: coreV1.PodStatus{
				Phase:      coreV1.PodPending,
				Conditions: []coreV1.PodCondition{{Type: coreV1.PodReady, Status: coreV1.ConditionTrue}},
			}},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, podReady(tc.pod))
		})
	}
}

// TestBackupExporterServicePort covers the "which Service port" decision: named "http", or the
// Service's only port regardless of its name.
func TestBackupExporterServicePort(t *testing.T) {
	t.Run("a single port is used regardless of its name", func(t *testing.T) {
		svc := &coreV1.Service{Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{{Name: "metrics", Port: 9090}},
		}}

		port, err := backupExporterServicePort(svc)
		require.NoError(t, err)
		assert.Equal(t, int32(9090), port.Port)
	})

	t.Run("multiple ports: the one named http is picked", func(t *testing.T) {
		svc := &coreV1.Service{Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{
				{Name: "metrics", Port: 9090},
				{Name: "http", Port: 8080},
			},
		}}

		port, err := backupExporterServicePort(svc)
		require.NoError(t, err)
		assert.Equal(t, int32(8080), port.Port)
	})

	t.Run("multiple ports, none named http, errors naming the service", func(t *testing.T) {
		svc := &coreV1.Service{
			ObjectMeta: metaV1.ObjectMeta{Name: "backup-exporter", Namespace: "monitoring"},
			Spec: coreV1.ServiceSpec{
				Ports: []coreV1.ServicePort{
					{Name: "metrics", Port: 9090},
					{Name: "admin", Port: 9091},
				},
			},
		}

		_, err := backupExporterServicePort(svc)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "monitoring/backup-exporter")
	})
}

// TestResolveTargetPort covers the "Service port -> container port" translation: numeric
// TargetPort, named TargetPort (found and missing), and an omitted TargetPort defaulting to
// Port - the same cases k8s.io/kubectl/pkg/util's target-port lookup handles.
func TestResolveTargetPort(t *testing.T) {
	pod := &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{Name: "backup-exporter-abc", Namespace: "monitoring"},
		Spec: coreV1.PodSpec{
			Containers: []coreV1.Container{
				{Ports: []coreV1.ContainerPort{{Name: "http", ContainerPort: 8080}}},
			},
		},
	}

	t.Run("numeric TargetPort is used directly", func(t *testing.T) {
		svc := &coreV1.Service{Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(8080)}},
		}}

		port, err := resolveTargetPort(svc, pod)
		require.NoError(t, err)
		assert.Equal(t, int32(8080), port)
	})

	t.Run("named TargetPort resolves against the pod's container ports", func(t *testing.T) {
		svc := &coreV1.Service{Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http")}},
		}}

		port, err := resolveTargetPort(svc, pod)
		require.NoError(t, err)
		assert.Equal(t, int32(8080), port)
	})

	t.Run("named TargetPort missing from the pod's containers errors", func(t *testing.T) {
		svc := &coreV1.Service{Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("metrics")}},
		}}

		_, err := resolveTargetPort(svc, pod)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"metrics"`)
	})

	t.Run("an omitted TargetPort defaults to the Service's Port", func(t *testing.T) {
		svc := &coreV1.Service{Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{{Name: "http", Port: 8080}},
		}}

		port, err := resolveTargetPort(svc, pod)
		require.NoError(t, err)
		assert.Equal(t, int32(8080), port)
	})

	t.Run("no eligible service port errors before looking at the pod at all", func(t *testing.T) {
		svc := &coreV1.Service{
			ObjectMeta: metaV1.ObjectMeta{Name: "backup-exporter", Namespace: "monitoring"},
			Spec: coreV1.ServiceSpec{
				Ports: []coreV1.ServicePort{
					{Name: "metrics", Port: 9090},
					{Name: "admin", Port: 9091},
				},
			},
		}

		_, err := resolveTargetPort(svc, pod)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "none named")
	})
}

// TestFetchBackupStatus exercises fetchBackupStatus's own sequencing (find Service -> find
// ready pod) up to but not including fetchViaPortForward: that needs a real pods/portforward
// stream and is deliberately left untested here, same reasoning as TestFindReadyPod's doc
// comment on why the fake clientset can't stand in for it. restConfig is never dereferenced
// on either path below (fetchViaPortForward is never reached), so an empty one is fine.
func TestFetchBackupStatus(t *testing.T) {
	t.Run("service discovery failure short-circuits before looking for a pod", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		_, err := fetchBackupStatus(context.Background(), clientset, &restclient.Config{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no backup-exporter service found")
	})

	t.Run("no ready pod behind the service short-circuits before resolving a port or forwarding", func(t *testing.T) {
		svc := &coreV1.Service{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      "backup-exporter",
				Namespace: "monitoring",
				Labels:    map[string]string{backupExporterLabelKey: backupExporterLabelValue},
			},
			Spec: coreV1.ServiceSpec{
				Selector: map[string]string{backupExporterLabelKey: backupExporterLabelValue},
				Ports:    []coreV1.ServicePort{{Name: "http", Port: 8080}},
			},
		}
		clientset := fake.NewSimpleClientset(svc)

		_, err := fetchBackupStatus(context.Background(), clientset, &restclient.Config{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no ready pod behind service monitoring/backup-exporter")
	})
}

// serverPort extracts the numeric port an httptest.Server is listening on, for
// getBackupsOverLocalPort's uint16 parameter. httptest.NewServer always binds 127.0.0.1,
// matching the fixed host getBackupsOverLocalPort itself targets.
func serverPort(t *testing.T, server *httptest.Server) uint16 {
	t.Helper()
	parsedURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseUint(parsedURL.Port(), 10, 16)
	require.NoError(t, err)
	return uint16(port)
}

// TestGetBackupsOverLocalPort covers the plain-HTTP half of the port-forward fetch in
// isolation, via a real (if local) HTTP server - unlike fetchViaPortForward/newPortForwardDialer,
// nothing here touches client-go's SPDY/WebSocket machinery, so it needs no fake-clientset
// workaround.
func TestGetBackupsOverLocalPort(t *testing.T) {
	t.Run("200 returns the body verbatim", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, backupExporterAPIPath, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"collectors":[],"resources":[]}`))
		}))
		defer server.Close()

		body, err := getBackupsOverLocalPort(context.Background(), serverPort(t, server))
		require.NoError(t, err)
		assert.JSONEq(t, `{"collectors":[],"resources":[]}`, string(body))
	})

	t.Run("non-200 status returns an error including the response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("collector not ready"))
		}))
		defer server.Close()

		_, err := getBackupsOverLocalPort(context.Background(), serverPort(t, server))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "503")
		assert.Contains(t, err.Error(), "collector not ready")
	})
}
