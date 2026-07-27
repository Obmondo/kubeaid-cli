// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextFreeVLANID(t *testing.T) {
	tests := []struct {
		name     string
		existing []robotVSwitch
		want     string
	}{
		{
			name: "empty account starts at the bottom of the range",
			want: "4000",
		},
		{
			name:     "skips taken IDs",
			existing: []robotVSwitch{{VLANID: 4000}, {VLANID: 4001}},
			want:     "4002",
		},
		{
			name:     "fills a hole rather than appending",
			existing: []robotVSwitch{{VLANID: 4000}, {VLANID: 4002}},
			want:     "4001",
		},
		{
			name:     "ignores IDs outside Hetzner's range",
			existing: []robotVSwitch{{VLANID: 100}},
			want:     "4000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, nextFreeVLANID(tc.existing))
		})
	}
}

func TestNextFreeVLANIDWhenRangeExhausted(t *testing.T) {
	existing := make([]robotVSwitch, 0, maxHetznerVLANID-minHetznerVLANID+1)
	for vlanID := minHetznerVLANID; vlanID <= maxHetznerVLANID; vlanID++ {
		existing = append(existing, robotVSwitch{VLANID: vlanID})
	}

	// Nothing is free — the operator still gets an editable default
	// and Robot supplies the real error.
	assert.Equal(t, "4000", nextFreeVLANID(existing))
}

func TestVLANIDFree(t *testing.T) {
	existing := []robotVSwitch{
		{Name: "other-cluster-vswitch", VLANID: 4000},
		{Name: "mine-vswitch", VLANID: 4005},
	}

	tests := []struct {
		name        string
		vSwitchName string
		input       string
		wantErr     bool
		errPart     string
	}{
		{
			name:        "free VLAN passes",
			vSwitchName: "new-vswitch",
			input:       "4010",
		},
		{
			// CreateVSwitch adopts a vSwitch matching name + VLAN ID,
			// so this combination is legitimate reuse, not a clash.
			name:        "own name on its own VLAN passes",
			vSwitchName: "mine-vswitch",
			input:       "4005",
		},
		{
			name:        "VLAN taken by another vSwitch is rejected",
			vSwitchName: "new-vswitch",
			input:       "4000",
			wantErr:     true,
			errPart:     `already used by vSwitch "other-cluster-vswitch"`,
		},
		{
			name:        "out-of-range VLAN still fails the range check",
			vSwitchName: "new-vswitch",
			input:       "3999",
			wantErr:     true,
			errPart:     "4000-4091",
		},
		{
			name:        "non-numeric is rejected",
			vSwitchName: "new-vswitch",
			input:       "four thousand",
			wantErr:     true,
			errPart:     "numeric",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &PromptedConfig{HetznerVSwitchName: tc.vSwitchName}

			err := vlanIDFree(cfg, existing)(tc.input)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errPart)
		})
	}
}

func TestVLANIDHelp(t *testing.T) {
	assert.Equal(t, "Hetzner only accepts 4000-4091.", vlanIDHelp(nil))
	assert.Equal(t,
		"Hetzner only accepts 4000-4091. Already in use: 4000, 4005.",
		vlanIDHelp([]robotVSwitch{{VLANID: 4000}, {VLANID: 4005}}),
	)
}

func TestRobotClientVSwitchList(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    []robotVSwitch
		wantErr string
	}{
		{
			name:   "returns the account's vSwitches",
			status: http.StatusOK,
			body:   `[{"id":1,"name":"a","vlan":4000,"cancelled":false},{"id":2,"name":"b","vlan":4001,"cancelled":false}]`,
			want: []robotVSwitch{
				{ID: 1, Name: "a", VLANID: 4000},
				{ID: 2, Name: "b", VLANID: 4001},
			},
		},
		{
			// A cancelled vSwitch can't be adopted — CreateVSwitch
			// hard-fails on one, so it must not reach the picker.
			name:   "drops cancelled vSwitches",
			status: http.StatusOK,
			body:   `[{"id":1,"name":"a","vlan":4000,"cancelled":true},{"id":2,"name":"b","vlan":4001,"cancelled":false}]`,
			want:   []robotVSwitch{{ID: 2, Name: "b", VLANID: 4001}},
		},
		{
			name:   "empty account returns no vSwitches",
			status: http.StatusOK,
			body:   `[]`,
			want:   []robotVSwitch{},
		},
		{
			name:    "401 is reported as bad credentials",
			status:  http.StatusUnauthorized,
			body:    `{}`,
			wantErr: "rejected (401)",
		},
		{
			name:    "other statuses are surfaced",
			status:  http.StatusInternalServerError,
			body:    `{}`,
			wantErr: "unexpected Robot status 500",
		},
		{
			name:    "malformed JSON is reported",
			status:  http.StatusOK,
			body:    `{nope`,
			wantErr: "decoding Robot vSwitch list response",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/vswitch", r.URL.Path)
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			got, err := robotClientVSwitchList(resty.New().SetBaseURL(server.URL))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFetchVSwitchesWithSpinnerUsesCache(t *testing.T) {
	cached := []robotVSwitch{{ID: 7, Name: "cached", VLANID: 4007}}
	cfg := &PromptedConfig{HetznerBMKnownVSwitches: cached}

	robotVSwitchListOverride = func() ([]robotVSwitch, error) {
		t.Error("Robot must not be re-queried when the vSwitch list is already cached")
		return nil, nil
	}
	defer func() { robotVSwitchListOverride = nil }()

	assert.Equal(t, cached, fetchVSwitchesWithSpinner(cfg))
}

func TestFetchVSwitchesWithSpinnerDegradesOnError(t *testing.T) {
	cfg := &PromptedConfig{}

	robotVSwitchListOverride = func() ([]robotVSwitch, error) {
		return nil, assert.AnError
	}
	defer func() { robotVSwitchListOverride = nil }()

	// A failed fetch must not block the prompt — the operator falls
	// back to typing the vSwitch details themselves.
	assert.Nil(t, fetchVSwitchesWithSpinner(cfg))
	assert.Nil(t, cfg.HetznerBMKnownVSwitches)
}
