// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package docker

import (
	"context"
	"log/slog"
	"os"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/term"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (d *Docker) PullImage(ctx context.Context, ref string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("ref", ref),
	})

	slog.InfoContext(ctx, "Pulling container image")

	pullProgressReader, err := d.client.ImagePull(ctx, ref, image.PullOptions{})
	assert.AssertErrNil(ctx, err, "Failed pulling container image")
	defer pullProgressReader.Close()

	stdoutFD, isTerminal := term.GetFdInfo(os.Stdout)
	_ = jsonmessage.DisplayJSONMessagesStream(pullProgressReader, os.Stdout, stdoutFD, isTerminal, nil)
}
