// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/term"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/containerruntime"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func (d *Docker) PullImage(ctx context.Context, ref string, policy containerruntime.ImagePullPolicy) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("ref", ref),
		slog.String("pullPolicy", string(policy)),
	})

	_, err := d.client.ImageInspect(ctx, ref)
	imageExistsLocally := err == nil

	switch policy {
	case containerruntime.ImagePullPolicyNever:
		assert.Assert(ctx, imageExistsLocally,
			fmt.Sprintf("Image %s not found locally and pull policy is Never", ref))
		slog.InfoContext(ctx, "Using local container image")
		return

	case containerruntime.ImagePullPolicyIfNotPresent:
		if imageExistsLocally {
			slog.InfoContext(ctx, "Container image already exists locally, skipping pull")
			return
		}

	case containerruntime.ImagePullPolicyAlways:
		// Always pull, fall through.
	}

	slog.InfoContext(ctx, "Pulling container image")

	pullProgressReader, err := d.client.ImagePull(ctx, ref, image.PullOptions{})
	assert.AssertErrNil(ctx, err, "Failed pulling container image")
	defer pullProgressReader.Close()

	stdoutFD, isTerminal := term.GetFdInfo(os.Stdout)
	_ = jsonmessage.DisplayJSONMessagesStream(pullProgressReader, os.Stdout, stdoutFD, isTerminal, nil)
}
