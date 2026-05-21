// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"log/slog"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

func (a *Azure) DeleteResourceGroup(ctx context.Context) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("resource-group-name", a.resourceGroupName),
	})

	if err := a.deleteResourceGroupFn(ctx, a.resourceGroupName); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Deleted resources related to Workload Identity Provider")
	return nil
}
