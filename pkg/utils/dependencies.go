// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"fmt"
	"os/exec"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

type lookPathFunc func(file string) (string, error)

// Determines the runtime dependencies required by KubeAid Bootstrap Script, based on the
// cloud-provider being used.
// Returns an error if any of them are not found in PATH.
func EnsureRuntimeDependenciesInstalled() error {
	return ensureRuntimeDependenciesInstalled(globals.CloudProviderName, exec.LookPath)
}

func ensureRuntimeDependenciesInstalled(cloudProviderName string, lookPath lookPathFunc) error {
	dependencies := requiredRuntimeDependencies(cloudProviderName)
	if err := verifyRuntimeDependenciesInPath(dependencies, lookPath); err != nil {
		return fmt.Errorf("runtime dependency unavailable: %w", err)
	}
	return nil
}

func requiredRuntimeDependencies(cloudProviderName string) []string {
	dependencies := constants.CommonRuntimeDependencies
	switch cloudProviderName {
	default:
	}
	return dependencies
}

func verifyRuntimeDependenciesInPath(dependencies []string, lookPath lookPathFunc) error {
	for _, dependency := range dependencies {
		if err := verifyExecutableInPath(dependency, lookPath); err != nil {
			return err
		}
	}
	return nil
}

func verifyExecutableInPath(name string, lookPath lookPathFunc) error {
	if _, err := lookPath(name); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Returns an error if the given runtime dependency / executable isn't found in PATH.
func EnsureRuntimeDependencyInstalled(name string) error {
	return ensureRuntimeDependencyInstalled(name, exec.LookPath)
}

func ensureRuntimeDependencyInstalled(name string, lookPath lookPathFunc) error {
	if err := verifyExecutableInPath(name, lookPath); err != nil {
		return fmt.Errorf("runtime dependency unavailable: %w", err)
	}
	return nil
}
