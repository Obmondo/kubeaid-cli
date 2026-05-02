// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package netbird

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realJSONFixture is a trimmed copy of `netbird status --json` from a real
// 0.69.0 daemon. It exercises the field names exactly as the daemon emits
// them — if NetBird ever renames a field, this test catches it.
const realJSONFixture = `{
  "peers": {
    "total": 4,
    "connected": 2,
    "details": [
      {
        "fqdn": "k8s-staging.netbird.selfhosted",
        "netbirdIp": "100.92.10.1/32",
        "status": "Connected"
      },
      {
        "fqdn": "k8s-prod.netbird.selfhosted",
        "netbirdIp": "100.92.10.2/32",
        "status": "Connected"
      },
      {
        "fqdn": "k8s-dev.netbird.selfhosted",
        "netbirdIp": "100.92.10.3/32",
        "status": "Idle"
      },
      {
        "fqdn": "alice-laptop.netbird.selfhosted",
        "netbirdIp": "100.92.10.4/32",
        "status": "Connected"
      }
    ]
  },
  "daemonStatus": "Connected",
  "management": {
    "url": "https://netbird.obmondo.com:443",
    "connected": true,
    "error": ""
  }
}`

func TestFetchStatus(t *testing.T) {
	t.Parallel()

	t.Run("parses the live daemon JSON shape", func(t *testing.T) {
		t.Parallel()

		stub := func(ctx context.Context) ([]byte, error) {
			return []byte(realJSONFixture), nil
		}

		s, err := withStub(stub, func() (*Status, error) {
			return FetchStatus(context.Background())
		})
		require.NoError(t, err)

		assert.Equal(t, "Connected", s.DaemonStatus)
		assert.Equal(t, "https://netbird.obmondo.com:443", s.Management.URL)
		assert.True(t, s.Management.Connected)
		assert.Equal(t, 4, s.Peers.Total)
		assert.Equal(t, 2, s.Peers.Connected)
		require.Len(t, s.Peers.Details, 4)
		assert.Equal(t, "k8s-staging.netbird.selfhosted", s.Peers.Details[0].FQDN)
		assert.Equal(t, "Connected", s.Peers.Details[0].Status)
	})

	t.Run("returns wrapped error when netbird CLI fails", func(t *testing.T) {
		t.Parallel()

		want := errors.New("daemon down")
		stub := func(ctx context.Context) ([]byte, error) {
			return nil, want
		}

		_, err := withStub(stub, func() (*Status, error) {
			return FetchStatus(context.Background())
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, want)
	})

	t.Run("returns parse error on garbage JSON", func(t *testing.T) {
		t.Parallel()

		stub := func(ctx context.Context) ([]byte, error) {
			return []byte("{not valid"), nil
		}

		_, err := withStub(stub, func() (*Status, error) {
			return FetchStatus(context.Background())
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing netbird status JSON")
	})
}

func TestAccessibleClusters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status *Status
		prefix string
		suffix string
		want   []string
	}{
		{
			name: "filters by prefix, suffix, and connected status",
			status: &Status{Peers: PeersInfo{Details: []Peer{
				{FQDN: "k8s-staging.netbird.selfhosted", Status: "Connected"},
				{FQDN: "k8s-prod.netbird.selfhosted", Status: "Connected"},
				{FQDN: "k8s-dev.netbird.selfhosted", Status: "Idle"},      // dropped: not connected
				{FQDN: "laptop.netbird.selfhosted", Status: "Connected"},  // dropped: no k8s- prefix
				{FQDN: "k8s-other.netbird.cloud", Status: "Connected"},    // dropped: wrong suffix
			}}},
			prefix: "k8s-",
			suffix: ".netbird.selfhosted",
			want:   []string{"staging", "prod"},
		},
		{
			name:   "empty input returns empty (not nil) slice",
			status: &Status{},
			prefix: "k8s-",
			suffix: ".netbird",
			want:   []string{},
		},
		{
			name: "fqdn that is exactly prefix+suffix is dropped (no name)",
			status: &Status{Peers: PeersInfo{Details: []Peer{
				{FQDN: "k8s-.netbird", Status: "Connected"},
			}}},
			prefix: "k8s-",
			suffix: ".netbird",
			want:   []string{},
		},
		{
			name: "trims prefix and suffix to expose only the cluster name",
			status: &Status{Peers: PeersInfo{Details: []Peer{
				{FQDN: "cluster-foo.acme.example.com", Status: "Connected"},
			}}},
			prefix: "cluster-",
			suffix: ".acme.example.com",
			want:   []string{"foo"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := AccessibleClusters(tc.status, tc.prefix, tc.suffix)
			assert.Equal(t, tc.want, got)
		})
	}
}

// withStub swaps the package-level runNetbirdStatus for the duration of fn,
// then restores it. Centralised so tests don't reach into package state
// repeatedly.
func withStub[T any](stub func(context.Context) ([]byte, error), fn func() (T, error)) (T, error) {
	orig := runNetbirdStatus
	runNetbirdStatus = stub
	defer func() { runNetbirdStatus = orig }()

	return fn()
}
