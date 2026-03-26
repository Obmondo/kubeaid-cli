// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package docker

import (
	"context"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (d *Docker) CloseSocketConnection(ctx context.Context) {
	err := d.client.Close()
	if err != nil {
		slog.WarnContext(ctx, "Failed closing Docker socket connection", logger.Error(err))
	}
}
