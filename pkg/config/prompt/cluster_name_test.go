// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		errParts []string
	}{
		{
			name:  "plain lowercase name passes",
			input: "staging",
		},
		{
			name:  "dashes and digits pass",
			input: "prod-acme-com-01",
		},
		{
			name:     "empty is required",
			input:    "",
			wantErr:  true,
			errParts: []string{"required"},
		},
		{
			// The reason this validator exists: parser.validateClusterName
			// rejects dots, but only after the whole prompt has run.
			name:     "dotted name is rejected with a hint",
			input:    "prod.acme.com",
			wantErr:  true,
			errParts: []string{"dots", "-"},
		},
		{
			name:     "uppercase is rejected",
			input:    "Staging",
			wantErr:  true,
			errParts: []string{"lowercase"},
		},
		{
			name:     "leading dash is rejected",
			input:    "-staging",
			wantErr:  true,
			errParts: []string{"alphanumeric"},
		},
		{
			name:     "trailing dash is rejected",
			input:    "staging-",
			wantErr:  true,
			errParts: []string{"alphanumeric"},
		},
		{
			name:     "underscore is rejected",
			input:    "stag_ing",
			wantErr:  true,
			errParts: []string{"lowercase"},
		},
		{
			// Exact match only: the lowercase rule already rejects "Logs",
			// so unlike clusterdir.ValidateName no case-folding is needed.
			name:     "the reserved logs name is rejected",
			input:    "logs",
			wantErr:  true,
			errParts: []string{"reserved"},
		},
		{
			name:  "63 characters passes",
			input: strings.Repeat("a", 63),
		},
		{
			name:     "64 characters is rejected",
			input:    strings.Repeat("a", 64),
			wantErr:  true,
			errParts: []string{"63"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := clusterName(tc.input)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			for _, part := range tc.errParts {
				assert.Contains(t, err.Error(), part)
			}
		})
	}
}
