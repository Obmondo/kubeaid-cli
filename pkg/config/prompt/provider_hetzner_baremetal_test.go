// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerIDValidator(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{name: "valid numeric ID", input: "1234567", wantError: false},
		{name: "leading zero is fine", input: "0123456", wantError: false},
		{name: "trimmed whitespace", input: "  1234567 ", wantError: false},
		{name: "empty rejected", input: "", wantError: true},
		{name: "all spaces rejected", input: "   ", wantError: true},
		{name: "letters rejected", input: "abc1234", wantError: true},
		{name: "hyphen rejected", input: "12345-67", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := serverIDValidator(tc.input)
			if tc.wantError {
				require.Error(t, err, "expected validation to fail")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestIPv4Validator(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{name: "valid ipv4", input: "10.0.0.5", wantError: false},
		{name: "public ipv4", input: "1.2.3.4", wantError: false},
		{name: "trimmed whitespace", input: "  10.0.0.5  ", wantError: false},
		{name: "empty rejected", input: "", wantError: true},
		{name: "ipv6 rejected", input: "::1", wantError: true},
		{name: "hostname rejected", input: "example.com", wantError: true},
		{name: "out-of-range octets rejected", input: "256.0.0.1", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ipv4(tc.input)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRobotClientLookupErrorMapping(t *testing.T) {
	// Drives the per-server lookup error surface from a fake
	// robotServerLookup so the user-facing wording stays terse and
	// the right action keys (401 / 404) are present for the message
	// the operator sees in the failure note.
	t.Run("401 surfaces an auth-style hint", func(t *testing.T) {
		var lookup robotServerLookup = func(_ string) (*robotServerInfo, error) {
			return nil, errors.New("robot username/password rejected (401) — re-enter them")
		}
		_, err := lookup("1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("404 surfaces no-such-server", func(t *testing.T) {
		var lookup robotServerLookup = func(_ string) (*robotServerInfo, error) {
			return nil, errors.New("no such server in this Robot account (404)")
		}
		_, err := lookup("999")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("success returns hydrated info", func(t *testing.T) {
		var lookup robotServerLookup = func(id string) (*robotServerInfo, error) {
			return &robotServerInfo{
				ID:       id,
				PublicIP: "1.2.3.4",
				Name:     "kbm-c01",
				Product:  "AX52",
				DC:       "hel1-dc4",
				Status:   "ready",
			}, nil
		}
		info, err := lookup("1234567")
		require.NoError(t, err)
		assert.Equal(t, "kbm-c01", info.Name)
		assert.Equal(t, "1.2.3.4", info.PublicIP)
	})
}

func TestRenderServerInfo(t *testing.T) {
	cases := []struct {
		name string
		info *robotServerInfo
		want string
	}{
		{
			name: "nil info",
			info: nil,
			want: "(Robot returned no metadata)",
		},
		{
			name: "fully populated",
			info: &robotServerInfo{
				Name:      "kbm-c01",
				Product:   "AX52",
				DC:        "hel1-dc4",
				PublicIP:  "1.2.3.4",
				PaidUntil: "2027-03-15",
			},
			want: "✓ kbm-c01 — AX52 — hel1-dc4 — main IP 1.2.3.4 — paid until 2027-03-15",
		},
		{
			name: "missing name and DC",
			info: &robotServerInfo{
				Product:  "AX52",
				PublicIP: "1.2.3.4",
			},
			want: "✓ AX52 — main IP 1.2.3.4",
		},
		{
			name: "empty info object",
			info: &robotServerInfo{},
			want: "(Robot returned no usable metadata)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, renderServerInfo(tc.info))
		})
	}
}

func TestValidateCPTopology(t *testing.T) {
	cases := []struct {
		name      string
		ids       []string
		wantError string
	}{
		{name: "single CP allowed", ids: []string{"1"}, wantError: ""},
		{name: "three CPs allowed", ids: []string{"1", "2", "3"}, wantError: ""},
		{name: "five CPs allowed", ids: []string{"1", "2", "3", "4", "5"}, wantError: ""},
		{name: "zero rejected", ids: nil, wantError: "at least one"},
		{name: "two rejected (no quorum win)", ids: []string{"1", "2"}, wantError: "must be odd"},
		{name: "four rejected (no quorum win)", ids: []string{"1", "2", "3", "4"}, wantError: "must be odd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCPTopology(&PromptedConfig{HetznerBMCPServerIDs: tc.ids})
			if tc.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}

func TestValidateWorkerTopology(t *testing.T) {
	t.Run("rejects empty workers", func(t *testing.T) {
		err := validateWorkerTopology(&PromptedConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one worker")
	})
	t.Run("accepts one worker", func(t *testing.T) {
		err := validateWorkerTopology(&PromptedConfig{HetznerBMNodeGroupServerIDs: []string{"1"}})
		require.NoError(t, err)
	})
}

func TestScanSiblingConfigsForServerIDs(t *testing.T) {
	t.Run("empty configs directory returns nil", func(t *testing.T) {
		assert.Nil(t, scanSiblingConfigsForServerIDs(""))
	})

	t.Run("non-existent parent returns nil", func(t *testing.T) {
		assert.Nil(t, scanSiblingConfigsForServerIDs("/does/not/exist/foo"))
	})

	t.Run("scans siblings, skips self, returns serverID -> cluster map", func(t *testing.T) {
		parent := t.TempDir()
		selfDir := filepath.Join(parent, "kbm")
		stagingDir := filepath.Join(parent, "staging")
		prodDir := filepath.Join(parent, "prod")
		require.NoError(t, os.MkdirAll(selfDir, 0o750))
		require.NoError(t, os.MkdirAll(stagingDir, 0o750))
		require.NoError(t, os.MkdirAll(prodDir, 0o750))

		// Self should be skipped — even if it has serverIDs from a
		// prior run, those don't count as a conflict.
		selfYAML := `cloud:
  hetzner:
    controlPlane:
      bareMetal:
        bareMetalHosts:
          - serverID: "999"
`
		require.NoError(t, os.WriteFile(filepath.Join(selfDir, "general.yaml"), []byte(selfYAML), 0o600))

		// Sibling clusters with overlapping IDs.
		stagingYAML := `cloud:
  hetzner:
    controlPlane:
      bareMetal:
        bareMetalHosts:
          - serverID: "100"
          - serverID: "101"
    nodeGroups:
      bareMetal:
        - bareMetalHosts:
            - serverID: "200"
`
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "general.yaml"), []byte(stagingYAML), 0o600))

		prodYAML := `cloud:
  hetzner:
    controlPlane:
      bareMetal:
        bareMetalHosts:
          - serverID: "300"
`
		require.NoError(t, os.WriteFile(filepath.Join(prodDir, "general.yaml"), []byte(prodYAML), 0o600))

		got := scanSiblingConfigsForServerIDs(selfDir)
		assert.Equal(t, "staging", got["100"])
		assert.Equal(t, "staging", got["101"])
		assert.Equal(t, "staging", got["200"])
		assert.Equal(t, "prod", got["300"])
		_, selfFound := got["999"]
		assert.False(t, selfFound, "self directory must be skipped")
	})

	t.Run("malformed YAML is silently skipped", func(t *testing.T) {
		parent := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(parent, "kbm"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(parent, "broken"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(parent, "broken", "general.yaml"),
			[]byte("this: is: not: valid: yaml: : :\n"), 0o600))
		got := scanSiblingConfigsForServerIDs(filepath.Join(parent, "kbm"))
		assert.Empty(t, got)
	})
}

func TestAppendServerAndNextIndex(t *testing.T) {
	cfg := &PromptedConfig{HetznerBMServerPublicIPs: map[string]string{}}

	assert.Equal(t, 1, nextIndex(cfg, roleControlPlane))
	appendServer(cfg, roleControlPlane, "100", "10.0.0.1", &robotServerInfo{PublicIP: "5.5.5.1"})
	assert.Equal(t, 2, nextIndex(cfg, roleControlPlane))
	assert.Equal(t, []string{"100"}, cfg.HetznerBMCPServerIDs)
	assert.Equal(t, "5.5.5.1", cfg.HetznerBMServerPublicIPs["100"])

	assert.Equal(t, 1, nextIndex(cfg, roleWorker))
	appendServer(cfg, roleWorker, "200", "10.0.0.10", &robotServerInfo{PublicIP: "5.5.5.10"})
	assert.Equal(t, 2, nextIndex(cfg, roleWorker))
	assert.Equal(t, []string{"200"}, cfg.HetznerBMNodeGroupServerIDs)
}
