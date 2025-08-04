# syntax=docker/dockerfile:1

#--- Builder stage ---
FROM golang:1.24.5-alpine AS builder

WORKDIR /app

RUN --mount=type=bind,src=go.mod,target=go.mod \
  --mount=type=bind,src=go.sum,target=go.sum \
  --mount=type=cache,target=/go/pkg/mod  \
  go mod download

RUN --mount=type=bind,src=.,target=. \
  --mount=type=cache,target=/go/pkg/mod  \
  CGO_ENABLED=0 go build -ldflags="-s -w" -v -o /usr/local/bin/kubeaid-bootstrap-script ./cmd

#--- Dependencies layer ---
FROM alpine:3.22 AS runtime-dependencies-installer

WORKDIR /

RUN apk add wget curl unzip

COPY scripts/install-runtime-dependencies.sh /opt/install-runtime-dependencies.sh

# We want to bundle dependencies for all the cloud providers,
# into the KubeAid Bootstrap Script container image.
ENV CLOUD_PROVIDER=all

RUN /opt/install-runtime-dependencies.sh

#--- Packager stage ---
FROM alpine:3.22 AS packager

WORKDIR /

ENV PATH=$PATH:/usr/local/bin:/usr/bin:/bin

COPY --from=builder /usr/local/bin/kubeaid-bootstrap-script /usr/local/bin/kubeaid-bootstrap-script
COPY --from=runtime-dependencies-installer /usr/local/bin /usr/local/bin
COPY --from=runtime-dependencies-installer /etc/ssl/ /etc/ssl

ENTRYPOINT [ "kubeaid-bootstrap-script" ]
