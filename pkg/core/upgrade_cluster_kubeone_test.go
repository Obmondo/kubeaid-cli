// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateK8sVersionHop(t *testing.T) {
	testCases := []struct {
		name           string
		currentVersion string
		targetVersion  string

		expectedErr    error
		expectErr      bool
		errMustContain string
	}{
		{
			name:           "already at target",
			currentVersion: "v1.34.1",
			targetVersion:  "v1.34.1",
			expectedErr:    errK8sVersionAlreadyAtTarget,
		},
		{
			name:           "patch upgrade within same minor",
			currentVersion: "v1.34.1",
			targetVersion:  "v1.34.3",
		},
		{
			name:           "next minor",
			currentVersion: "v1.34.1",
			targetVersion:  "v1.35.0",
		},
		{
			name:           "next minor with lower patch number",
			currentVersion: "v1.34.5",
			targetVersion:  "v1.35.0",
		},
		{
			name:           "v prefix optional",
			currentVersion: "1.34.1",
			targetVersion:  "v1.35.2",
		},
		{
			name:           "patch downgrade",
			currentVersion: "v1.34.3",
			targetVersion:  "v1.34.1",
			expectErr:      true,
			errMustContain: "downgrade",
		},
		{
			name:           "minor downgrade",
			currentVersion: "v1.35.0",
			targetVersion:  "v1.34.5",
			expectErr:      true,
			errMustContain: "downgrade",
		},
		{
			name:           "skipping a minor",
			currentVersion: "v1.33.2",
			targetVersion:  "v1.35.0",
			expectErr:      true,
			errMustContain: "v1.34",
		},
		{
			name:           "major jump",
			currentVersion: "v1.34.1",
			targetVersion:  "v2.0.0",
			expectErr:      true,
		},
		{
			name:           "garbage current version",
			currentVersion: "one.two.three",
			targetVersion:  "v1.35.0",
			expectErr:      true,
		},
		{
			name:           "garbage target version",
			currentVersion: "v1.34.1",
			targetVersion:  "",
			expectErr:      true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateK8sVersionHop(testCase.currentVersion, testCase.targetVersion)

			if testCase.expectedErr != nil {
				require.ErrorIs(t, err, testCase.expectedErr)
				return
			}

			if !testCase.expectErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			if len(testCase.errMustContain) > 0 {
				assert.Contains(t, err.Error(), testCase.errMustContain)
			}
		})
	}
}
