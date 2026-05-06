// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/commandexecutor"
)

// kubePromBuilderDockerfile is the Dockerfile content used to build
// the small image that runs build.sh. Embedding it in the binary
// keeps kubeaid-cli self-contained: a fresh checkout doesn't need
// any extra files on disk to bootstrap the image, and operators
// don't have to manually maintain an image registry tag.
//
//go:embed kube_prom_builder.Dockerfile
var kubePromBuilderDockerfile string

// ensureKubePromBuilderImage builds the kube-prom-builder image if
// it isn't already present locally. docker's layer cache makes
// repeated invocations effectively free — apk + go install layers
// are content-hashed against the Dockerfile, so changes to the
// Dockerfile rebuild only what changed.
//
// Writes the embedded Dockerfile to a temp dir and uses that as the
// build context. We don't need any additional files in the context;
// everything build.sh needs is bind-mounted at runtime.
func ensureKubePromBuilderImage(ctx context.Context) {
	slog.InfoContext(ctx, "Ensuring kube-prom-builder docker image is built...")

	tempDir, err := os.MkdirTemp("", "kube-prom-builder-*")
	assert.AssertErrNil(ctx, err, "Failed creating temp dir for Dockerfile")
	defer os.RemoveAll(tempDir)

	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	err = os.WriteFile(dockerfilePath, []byte(kubePromBuilderDockerfile), 0o600)
	assert.AssertErrNil(ctx, err, "Failed writing Dockerfile to temp dir")

	commandexecutor.NewLocalCommandExecutor(false).MustExecute(ctx,
		fmt.Sprintf("docker build -t %s %s", constants.KubePromBuilderImage, tempDir),
	)
}
