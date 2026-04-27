// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
		wantError bool
		contains  []string
		absent    []string
	}{
		{
			name:      "past_eol",
			now:       date(2025, 1, 1),
			version:   "v9.0.0",
			wantError: true,
			contains:  []string{"K8s 9.0 reached EOL on 2000-01-01"},
		},
		{
			name:     "near_support_end",
			now:      date(2030, 1, 15),
			version:  "v9.1.0",
			contains: []string{"leaves active support"},
			absent:   []string{"reaches EOL"},
		},
		{
			name:     "near_eol",
			now:      date(2030, 2, 1),
			version:  "v9.2.0",
			contains: []string{"reaches EOL"},
			absent:   []string{"leaves active support"},
		},
		{
			name:      "unknown_cycle",
			now:       date(2030, 1, 1),
			version:   "v8.0.0",
			wantError: true,
			contains:  []string{"not supported by kubeaid-cli"},
		},
		{
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
			k8sLatest:       "",
			userVersion:     "v1.35.0",
			wantErr:         true,
			wantErrContains: "stub network error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLatestStableK8sReleaseFn(t)

			if tc.k8sLatest == "" {
				latestStableK8sReleaseFn = func() (string, error) {
					return "", fmt.Errorf("stub network error")
				}
			} else {
				latest := tc.k8sLatest
				latestStableK8sReleaseFn = func() (string, error) {
					return latest, nil
				}
			}

			err := checkK8sNotReleased(tc.userVersion)

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

func TestParseK8sLifecycles(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    map[string]k8sLifecycle
		wantErr bool
	}{
		{
			name: "fixture parses into a cycle map",
			data: k8sEOLFixtureData,
			want: map[string]k8sLifecycle{
				"9.0": {Cycle: "9.0", EOL: "2000-01-01", Support: ""},
				"9.1": {Cycle: "9.1", EOL: "2030-12-01", Support: "2030-03-01"},
				"9.2": {Cycle: "9.2", EOL: "2030-03-01", Support: "2030-01-01"},
				"9.3": {Cycle: "9.3", EOL: "2031-06-01", Support: "2030-12-01"},
			},
		},
		{
			name:    "malformed JSON returns wrapped error",
			data:    []byte("{not json"),
			wantErr: true,
		},
		{
			name: "empty array yields empty map",
			data: []byte("[]"),
			want: map[string]k8sLifecycle{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseK8sLifecycles(tc.data)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLatestStableK8sRelease(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantVersion string
		wantErr     bool
	}{
		{
			name: "trims surrounding whitespace from response body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "  v1.34.2\n")
			},
			wantVersion: "v1.34.2",
		},
		{
			name: "non-200 response surfaces as error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			origURL := k8sReleaseAPIURL
			t.Cleanup(func() { k8sReleaseAPIURL = origURL })
			k8sReleaseAPIURL = server.URL

			got, err := latestStableK8sRelease()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantVersion, got)
		})
	}
}
