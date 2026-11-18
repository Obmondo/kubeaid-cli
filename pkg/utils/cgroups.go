// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
)

// Returns whether the CGroup version on the host machine is 2.
func IsCGroupV2() (bool, error) {
	_, err := os.Stat("/sys/fs/cgroup/cgroup.controllers")
	if errors.Is(err, os.ErrNotExist) {
		slog.Info("Detected CGroup legacy v1")
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("determining CGroup version: %w", err)
	}

	return true, nil
}
