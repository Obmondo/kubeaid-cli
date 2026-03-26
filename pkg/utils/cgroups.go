// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns whether the CGroup version on the host machine is 2.
func IsCGroupV2() bool {
	_, err := os.Stat("/sys/fs/cgroup/cgroup.controllers")
	if errors.Is(err, os.ErrNotExist) {
		slog.Info("Detected CGroup legacy v1")
		return false
	}
	assert.AssertErrNil(context.Background(), err, "Failed determining CGroup version")

	return true
}
