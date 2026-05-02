// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMinorVersion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMinor int
		wantErr   bool
	}{
		{name: "v-prefixed semver", input: "v1.35.1", wantMinor: 35},
		{name: "no v prefix", input: "1.32.0", wantMinor: 32},
		{name: "two-segment version", input: "v1.34", wantMinor: 34},
		{name: "double-digit minor", input: "v1.100.0", wantMinor: 100},
		{name: "missing minor segment", input: "v1", wantErr: true},
		{name: "non-numeric minor", input: "v1.x.0", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMinorVersion(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantMinor, got)
		})
	}
}
