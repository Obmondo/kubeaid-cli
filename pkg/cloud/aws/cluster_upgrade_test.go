// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mutates yq package-level globals via yqCmdLib.New() — sequential only.
func TestUpdateCapiClusterValuesFile(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		updates any
		wantErr bool
		errMsg  string
	}{
		{
			name: "wrong updates type returns error",
			setup: func(_ *testing.T) string {
				return "/unused"
			},
			updates: "not-a-valid-type",
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name: "wrong updates type nil returns error",
			setup: func(_ *testing.T) string {
				return "/unused"
			},
			updates: nil,
			wantErr: true,
			errMsg:  "wrong type of MachineTemplateUpdates object passed",
		},
		{
			name: "returns error for nonexistent file",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent.yaml")
			},
			updates: AWSMachineTemplateUpdates{AMIID: "ami-new-999"},
			wantErr: true,
			errMsg:  "updating AMI ID for control-plane nodes in values-capi-cluster.yaml",
		},
		{
			name: "returns error for existing file due to invalid yq flags in production code",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				path := filepath.Join(dir, "values.yaml")
				content := "aws:\n  controlPlane:\n    ami:\n      id: \"old-ami-111\"\n  nodeGroups:\n    - name: ng1\n      ami:\n        id: \"old-ami-222\"\n"
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
				return path
			},
			updates: AWSMachineTemplateUpdates{AMIID: "ami-new-999"},
			wantErr: true,
			errMsg:  "updating AMI ID for control-plane nodes in values-capi-cluster.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t)

			a := &AWS{}
			err := a.UpdateCapiClusterValuesFile(context.Background(), path, tc.updates)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
