# Image used by kubeaid-cli's buildKubePrometheus to run KubeAid's
# build/kube-prometheus/build.sh inside a sandboxed environment with
# the jsonnet toolchain pre-installed. Lets the rest of kubeaid-cli
# stay a single binary on the host while the build-time deps
# (jsonnet, jb, gojsontoyaml, jq, util-linux's column) live only in
# this small image.
#
# kubeaid-cli embeds this file at build time and feeds it to
# `docker build` over stdin on first run; subsequent runs hit the
# docker layer cache.

FROM alpine:3.22

RUN apk add --no-cache \
      bash \
      git \
      jq \
      util-linux \
      go \
      ca-certificates

# jsonnet (go-jsonnet by Google).
RUN go install github.com/google/go-jsonnet/cmd/jsonnet@v0.21.0 && \
    mv /root/go/bin/jsonnet /usr/local/bin/

# jsonnet-bundler (jb).
RUN go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@v0.6.0 && \
    mv /root/go/bin/jb /usr/local/bin/

# gojsontoyaml.
RUN go install github.com/brancz/gojsontoyaml@v0.1.0 && \
    mv /root/go/bin/gojsontoyaml /usr/local/bin/

# Drop go now that the binaries are baked in — saves ~400MB.
RUN apk del go && rm -rf /root/go /root/.cache /var/cache/apk/*

# Run as a non-root user so files created in the bind-mounted
# cluster_dir end up with the operator's uid (kubeaid-cli passes
# --user $(id -u):$(id -g) at runtime).
WORKDIR /work
ENTRYPOINT ["bash"]
