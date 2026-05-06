// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"fmt"
	"os/exec"
)

type lookPathFunc func(file string) (string, error)

func verifyExecutableInPath(name string, lookPath lookPathFunc) error {
	if _, err := lookPath(name); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Returns an error if the given runtime dependency / executable isn't found in PATH.
func EnsureRuntimeDependencyInstalled(_ context.Context, name string) error {
	if err := verifyExecutableInPath(name, exec.LookPath); err != nil {
		return fmt.Errorf("runtime dependency unavailable: %w", err)
	}
	return nil
}
