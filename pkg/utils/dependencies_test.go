// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func TestVerifyRuntimeDependencies(t *testing.T) {
	tests := []struct {
		name      string
		run       func(t *testing.T) error
		wantErr   bool
		wantIsErr error
		wantErrs  []string
	}{
		{
			name: "verify dependencies in path uses injected lookpath",
			run: func(t *testing.T) error {
				lookedUp := []string{}
				lookPath := func(file string) (string, error) {
					lookedUp = append(lookedUp, file)
					return "/usr/bin/" + file, nil
				}

				err := verifyRuntimeDependenciesInPath([]string{"kubectl", "jq"}, lookPath)

				assert.Equal(t, []string{"kubectl", "jq"}, lookedUp)
				return err
			},
		},
		{
			name: "verify dependencies in path returns missing dependency error",
			run: func(t *testing.T) error {
				missingErr := errors.New("executable file not found in PATH")
				lookPath := func(file string) (string, error) {
					if file == "jq" {
						return "", missingErr
					}
					return "/usr/bin/" + file, nil
				}

				err := verifyRuntimeDependenciesInPath([]string{"kubectl", "jq", "jsonnet"}, lookPath)

				assert.ErrorIs(t, err, missingErr)
				return err
			},
			wantErr:  true,
			wantErrs: []string{"jq"},
		},
		{
			name: "verify dependencies in path stops after first missing dependency",
			run: func(t *testing.T) error {
				lookedUp := []string{}
				lookPath := func(file string) (string, error) {
					lookedUp = append(lookedUp, file)
					if file == "jq" {
						return "", errors.New("missing")
					}
					return "/usr/bin/" + file, nil
				}

				err := verifyRuntimeDependenciesInPath([]string{"kubectl", "jq", "jsonnet"}, lookPath)

				assert.Equal(t, []string{"kubectl", "jq"}, lookedUp)
				return err
			},
			wantErr: true,
		},
		{
			name: "verify executable in path uses injected lookpath",
			run: func(t *testing.T) error {
				lookedUp := ""
				lookPath := func(file string) (string, error) {
					lookedUp = file
					return "/usr/bin/" + file, nil
				}

				err := verifyExecutableInPath("kubectl", lookPath)

				assert.Equal(t, "kubectl", lookedUp)
				return err
			},
		},
		{
			name: "required dependencies returns common dependencies",
			run: func(t *testing.T) error {
				dependencies := requiredRuntimeDependencies("unsupported-provider")

				assert.Equal(t, constants.CommonRuntimeDependencies, dependencies)
				return nil
			},
		},
		{
			name: "ensure runtime dependencies installed uses injected provider and lookpath",
			run: func(t *testing.T) error {
				lookedUp := []string{}
				lookPath := func(file string) (string, error) {
					lookedUp = append(lookedUp, file)
					return "/usr/bin/" + file, nil
				}

				err := ensureRuntimeDependenciesInstalled("unsupported-provider", lookPath)

				assert.Equal(t, constants.CommonRuntimeDependencies, lookedUp)
				return err
			},
		},
		{
			name: "ensure runtime dependencies installed wraps missing dependency error",
			run: func(t *testing.T) error {
				missingErr := errors.New("missing executable")
				lookPath := func(file string) (string, error) {
					if file == constants.CommonRuntimeDependencies[0] {
						return "", missingErr
					}
					return "/usr/bin/" + file, nil
				}

				err := ensureRuntimeDependenciesInstalled("unsupported-provider", lookPath)

				assert.ErrorIs(t, err, missingErr)
				return err
			},
			wantErr: true,
			wantErrs: []string{
				"runtime dependency unavailable",
				constants.CommonRuntimeDependencies[0],
			},
		},
		{
			name: "ensure runtime dependency installed uses injected lookpath",
			run: func(t *testing.T) error {
				lookedUp := ""
				lookPath := func(file string) (string, error) {
					lookedUp = file
					return "/usr/bin/" + file, nil
				}

				err := ensureRuntimeDependencyInstalled("kubectl", lookPath)

				assert.Equal(t, "kubectl", lookedUp)
				return err
			},
		},
		{
			name: "ensure runtime dependency installed wraps missing dependency error",
			run: func(t *testing.T) error {
				missingErr := errors.New("missing executable")
				lookPath := func(string) (string, error) {
					return "", missingErr
				}

				err := ensureRuntimeDependencyInstalled("kubectl", lookPath)

				assert.ErrorIs(t, err, missingErr)
				return err
			},
			wantErr:  true,
			wantErrs: []string{"runtime dependency unavailable", "kubectl"},
		},
		{
			name: "ensure runtime dependency installed returns error for missing executable",
			run: func(t *testing.T) error {
				return EnsureRuntimeDependencyInstalled("definitely-not-a-kubeaid-runtime-dependency")
			},
			wantErr: true,
			wantErrs: []string{
				"runtime dependency unavailable",
				"definitely-not-a-kubeaid-runtime-dependency",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(t)
			if tc.wantErr {
				require.Error(t, err)
				if tc.wantIsErr != nil {
					assert.ErrorIs(t, err, tc.wantIsErr)
				}
				for _, wantErr := range tc.wantErrs {
					assert.Contains(t, err.Error(), wantErr)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}
