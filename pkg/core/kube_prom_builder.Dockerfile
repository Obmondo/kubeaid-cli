# Image used by kubeaid-cli's buildKubePrometheus to run KubeAid's
# build/kube-prometheus/build.sh inside a sandboxed environment with
# the jsonnet toolchain pre-installed. Lets the rest of kubeaid-cli
# stay a single binary on the host while the build-time deps
# (jsonnet, jb, gojsontoyaml, jq, util-linux's column) live only in
# this small image.
#
# kubeaid-cli embeds this file at build time and feeds it to the
# Docker SDK's ImageBuild on first run; subsequent runs hit the
# docker layer cache.
#
# Multi-stage build: stage 1 has Go (~360 MB) only to build the
# jsonnet toolchain from source — Alpine's apk repo doesn't ship
# pinned versions of jb or gojsontoyaml, and we want exact versions
# to keep cluster-side rendering reproducible. Stage 2 is a clean
# alpine that COPY's just the three statically-linked binaries.
# Final image is ~135 MB on disk vs. ~605 MB if go stayed.
FROM golang:1.23-alpine AS builder

RUN go install github.com/google/go-jsonnet/cmd/jsonnet@v0.21.0 \
 && go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@v0.6.0 \
 && go install github.com/brancz/gojsontoyaml@v0.1.0


FROM alpine:3.22

RUN apk add --no-cache \
      bash \
      git \
      jq \
      util-linux \
      ca-certificates

COPY --from=builder /go/bin/jsonnet       /usr/local/bin/jsonnet
COPY --from=builder /go/bin/jb            /usr/local/bin/jb
COPY --from=builder /go/bin/gojsontoyaml  /usr/local/bin/gojsontoyaml

# Run as a non-root user so files created in the bind-mounted
# cluster_dir end up with the operator's uid (kubeaid-cli passes
# --user $(id -u):$(id -g) at runtime).
WORKDIR /work
ENTRYPOINT ["bash"]
