// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package azure

import (
	"context"
	"log/slog"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (a *Azure) DeleteResourceGroup(ctx context.Context) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("resource-group-name", a.resourceGroupName),
	})

	responsePoller, err := a.resourceGroupsClient.BeginDelete(ctx, a.resourceGroupName, nil)
	assert.AssertErrNil(ctx, err, "Failed deleting Azure Resource Group")

	_, err = responsePoller.PollUntilDone(ctx, nil)
	assert.AssertErrNil(ctx, err, "Failed deleting Azure Resource Group")

	slog.InfoContext(ctx, "Deleted resources related to Workload Identity Provider")
}
