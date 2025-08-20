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
  CGO_ENABLED=0 go build -ldflags="-s -w" -v -o /usr/local/bin/kubeaid-core ./cmd/kubeaid-core

#--- Dependencies layer ---
FROM alpine:3.22 AS runtime-dependencies-installer

RUN apk add --no-cache bash curl wget unzip 

COPY scripts/install-runtime-dependencies.sh /opt/install-runtime-dependencies.sh

# We want to bundle dependencies for all the cloud providers,
# into the KubeAid Bootstrap Script container image.
ENV CLOUD_PROVIDER=all

RUN /opt/install-runtime-dependencies.sh

#--- Packager stage ---
FROM alpine:3.22 AS packager

LABEL org.opencontainers.image.authors="archisman@obmondo.com,ashish@obmondo.com"
LABEL org.opencontainers.image.url="https://github.com/Obmondo/kubeaid-cli"
LABEL org.opencontainers.image.source="https://github.com/Obmondo/kubeaid-cli"
LABEL org.opencontainers.image.vendor="Obmondo"
LABEL org.opencontainers.image.licenses="GPL3"

ENV PATH=$PATH:/usr/local/bin:/usr/bin:/bin

RUN apk add --no-cache bash

COPY --from=builder /usr/local/bin/kubeaid-core /usr/local/bin/kubeaid-core
COPY --from=runtime-dependencies-installer /usr/local/bin /usr/local/bin

ENTRYPOINT [ "kubeaid-core" ]
