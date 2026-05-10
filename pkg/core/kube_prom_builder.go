// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"io"
	"log/slog"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
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
// Uses the Docker SDK directly (cli.ImageBuild) rather than shelling
// out to `docker build`. The shell-out path lost build failures to
// the same nil-error wrapping bug that bit runKubePrometheusBuilder
// — when bash exited non-zero with no stderr captured, the operator
// saw `error=: %!w(<nil>)` and zero diagnostic. Through the SDK,
// build failures arrive as structured JSON messages on the response
// stream and jsonmessage.DisplayJSONMessagesStream surfaces them as
// real Go errors with the failing build step's output included.
//
// Same client setup as runKubePrometheusBuilder; FromEnv picks up
// the operator's DOCKER_HOST / context / TLS configuration.
func ensureKubePromBuilderImage(ctx context.Context) {
	slog.InfoContext(ctx, "Ensuring kube-prom-builder docker image is built...")

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	assert.AssertErrNil(ctx, err, "Failed creating docker client")
	defer func() { _ = cli.Close() }()

	// Build an in-memory tar context with just the Dockerfile —
	// the image's RUN steps fetch everything they need from apk /
	// go (no COPY from local files), so a single-file context is
	// all the daemon needs.
	tarBuf := buildSingleFileTarContext(ctx, "Dockerfile", kubePromBuilderDockerfile)

	resp, err := cli.ImageBuild(ctx, tarBuf, build.ImageBuildOptions{
		Tags:       []string{constants.KubePromBuilderImage},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	assert.AssertErrNil(ctx, err, "Failed initiating docker image build")
	defer func() { _ = resp.Body.Close() }()

	// DisplayJSONMessagesStream drains the streaming build output.
	// Returns an error iff a message in the stream signals a
	// build-step failure (apk install, go install, network
	// time-out, etc.) — the structured error includes the failing
	// step's output, so the operator can act on it instead of
	// staring at an opaque exit code.
	//
	// io.Discard for the writer: we don't want to flood the
	// operator's terminal with FROM/RUN lines on a first-run
	// build. The slog DebugContext above is the audit trail; if
	// the build fails, the returned error has the diagnostic.
	err = jsonmessage.DisplayJSONMessagesStream(resp.Body, io.Discard, 0, false, nil)
	assert.AssertErrNil(ctx, err, "Docker image build failed")
}

// buildSingleFileTarContext returns a tar archive in memory
// containing one file at name with the given contents. Used to
// produce a minimal build context for cli.ImageBuild without
// touching the filesystem (no temp dir + os.WriteFile dance).
func buildSingleFileTarContext(ctx context.Context, name, contents string) *bytes.Buffer {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	assert.AssertErrNil(ctx, tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(contents)),
	}), "Failed writing tar header for build context")

	_, err := tw.Write([]byte(contents))
	assert.AssertErrNil(ctx, err, "Failed writing file contents to tar build context")

	assert.AssertErrNil(ctx, tw.Close(),
		"Failed closing tar writer for build context")
	return buf
}
