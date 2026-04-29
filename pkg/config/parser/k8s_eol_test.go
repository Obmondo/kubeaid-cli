// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/k8s-eol-fixture.json
var k8sEOLFixtureData []byte

func TestCheckK8sLifecycle(t *testing.T) {
	date := func(y, m, d int) time.Time {
		return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	}

	fixture, err := parseK8sLifecycles(k8sEOLFixtureData)
	require.NoError(t, err, "fixture must parse cleanly")

	cases := []struct {
		name      string
		now       time.Time
		version   string
		wantError bool     // true → checkK8sLifecycle must return non-nil error
		contains  []string // wantError → substrings expected in err.Error(); else in slog buffer
		absent    []string // substrings that must NOT appear in slog buffer
	}{
		{
			// 9.0 EOL was 2000-01-01; "now" of 2025-01-01 is well past.
			name:      "past_eol",
			now:       date(2025, 1, 1),
			version:   "v9.0.0",
			wantError: true,
			contains:  []string{"K8s 9.0 reached EOL on 2000-01-01"},
		},
		{
			// 9.1 active support ends 2030-03-01 (~45d after now); EOL 2030-12-01 (~320d, outside window).
			name:     "near_support_end",
			now:      date(2030, 1, 15),
			version:  "v9.1.0",
			contains: []string{"leaves active support"},
			absent:   []string{"reaches EOL"},
		},
		{
			// 9.2 support 2030-01-01 (already past); EOL 2030-03-01 (~28d ahead).
			name:     "near_eol",
			now:      date(2030, 2, 1),
			version:  "v9.2.0",
			contains: []string{"reaches EOL"},
			absent:   []string{"leaves active support"},
		},
		{
			// checkK8sLifecycle logs the info message and returns an error for unknown cycles.
			name:      "unknown_cycle",
			now:       date(2030, 1, 1),
			version:   "v8.0.0", // not in fixture
			wantError: true,
			contains:  []string{"not supported by kubeaid-cli"},
		},
		{
			// 9.3 support 2030-12-01 (>10 months); EOL 2031-06-01 (>17 months). Silent pass.
			name:    "healthy",
			now:     date(2030, 1, 1),
			version: "v9.3.0",
			absent:  []string{"reached EOL", "leaves active support", "reaches EOL"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetNowFn(t)
			resetLifecyclesFn(t)
			nowFn = func() time.Time { return tc.now }
			lifecyclesFn = func() (map[string]k8sLifecycle, error) { return fixture, nil }
			buf := captureSlog(t)

			err := checkK8sLifecycle(context.Background(), tc.version)

			if tc.wantError {
				require.Error(t, err)
				for _, want := range tc.contains {
					assert.Contains(t, err.Error(), want)
				}
				return
			}

			require.NoError(t, err)
			out := buf.String()
			for _, want := range tc.contains {
				assert.Contains(t, out, want)
			}
			for _, dont := range tc.absent {
				assert.NotContains(t, out, dont)
			}
		})
	}
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func resetNowFn(t *testing.T) {
	t.Helper()
	prev := nowFn
	t.Cleanup(func() { nowFn = prev })
}

func resetLifecyclesFn(t *testing.T) {
	t.Helper()
	prev := lifecyclesFn
	t.Cleanup(func() { lifecyclesFn = prev })
}

func resetLatestStableK8sReleaseFn(t *testing.T) {
	t.Helper()
	prev := latestStableK8sReleaseFn
	t.Cleanup(func() { latestStableK8sReleaseFn = prev })
}

func TestCheckK8sNotReleased(t *testing.T) {
	tests := []struct {
		name            string
		k8sLatest       string
		userVersion     string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:        "userVersion strictly less than latest — ok",
			k8sLatest:   "v1.35.1",
			userVersion: "v1.34.0",
		},
		{
			name:        "userVersion equal to latest — ok",
			k8sLatest:   "v1.35.1",
			userVersion: "v1.35.1",
		},
		{
			name:            "userVersion minor greater than latest — error",
			k8sLatest:       "v1.35.1",
			userVersion:     "v1.36.0",
			wantErr:         true,
			wantErrContains: "not released",
		},
		{
			name:            "userVersion major.minor greatly ahead — error",
			k8sLatest:       "v1.35.1",
			userVersion:     "v1.99.0",
			wantErr:         true,
			wantErrContains: "not released",
		},
		{
			name:            "userVersion patch greater within same minor — error",
			k8sLatest:       "v1.35.1",
			userVersion:     "v1.35.99",
			wantErr:         true,
			wantErrContains: "not released",
		},
		{
			name:            "stub fetch returns error — propagated",
			k8sLatest:       "", // signals error
			userVersion:     "v1.35.0",
			wantErr:         true,
			wantErrContains: "stub network error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLatestStableK8sReleaseFn(t)

			if tc.k8sLatest == "" {
				latestStableK8sReleaseFn = func(ctx context.Context) (string, error) {
					return "", fmt.Errorf("stub network error")
				}
			} else {
				latest := tc.k8sLatest
				latestStableK8sReleaseFn = func(ctx context.Context) (string, error) {
					return latest, nil
				}
			}

			err := checkK8sNotReleased(context.Background(), tc.userVersion)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrContains != "" {
					assert.Contains(t, err.Error(), tc.wantErrContains)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}
