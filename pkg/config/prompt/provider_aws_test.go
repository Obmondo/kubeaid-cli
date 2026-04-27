// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSPrompter_SummaryLines(t *testing.T) {
	tests := []struct {
		name string
		cfg  *PromptedConfig
		want []string
	}{
		{
			name: "all fields populated",
			cfg: &PromptedConfig{
				AWSRegion:         "eu-west-1",
				AWSCPInstanceType: "t3.medium",
				AWSCPReplicas:     "3",
			},
			want: []string{
				"  Region:        eu-west-1",
				"  Instance type: t3.medium",
				"  CP replicas:   3",
			},
		},
		{
			name: "empty values still render",
			cfg:  &PromptedConfig{},
			want: []string{
				"  Region:        ",
				"  Instance type: ",
				"  CP replicas:   ",
			},
		},
	}

	p := newAWSProvider()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.SummaryLines(tc.cfg))
		})
	}
}

func TestDetectAWSCredentials(t *testing.T) {
	tests := []struct {
		name      string
		setupHome func(t *testing.T) string
		wantOK    bool
		wantBase  string
	}{
		{
			name: "credentials file is detected",
			setupHome: func(t *testing.T) string {
				home := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o700))
				require.NoError(t, os.WriteFile(
					filepath.Join(home, ".aws", "credentials"),
					[]byte("[default]\n"),
					0o600,
				))
				return home
			},
			wantOK:   true,
			wantBase: "credentials",
		},
		{
			name: "config file is detected when credentials missing",
			setupHome: func(t *testing.T) string {
				home := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o700))
				require.NoError(t, os.WriteFile(
					filepath.Join(home, ".aws", "config"),
					[]byte("[default]\nregion=eu-west-1\n"),
					0o600,
				))
				return home
			},
			wantOK:   true,
			wantBase: "config",
		},
		{
			name: "no AWS files means not detected",
			setupHome: func(t *testing.T) string {
				return t.TempDir()
			},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := tc.setupHome(t)
			t.Setenv("HOME", home)

			source, ok := detectAWSCredentials()
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantBase, filepath.Base(source))
			}
		})
	}
}
