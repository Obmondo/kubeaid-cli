# syntax=docker/dockerfile:1

#--- Builder stage ---
FROM golang:1.24.5 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -v -o kubeaid-bootstrap-script ./cmd

#--- Dependencies layer ---
FROM ubuntu:24.04 AS dependencies
WORKDIR /
COPY ./scripts/install-runtime-dependencies.sh /install-runtime-dependencies.sh
RUN apt update
RUN apt install -y wget curl unzip
RUN chmod +x /install-runtime-dependencies.sh && \
    /install-runtime-dependencies.sh

#--- Packager stage ---
FROM scratch
WORKDIR /
ENV PATH=$PATH:/obmondo/bin:/bin
# ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
LABEL org.opencontainers.image.authors="ashish@obmondo.com, archisman@obmondo.com"
WORKDIR /

COPY --from=dependencies /bin /bin
COPY --from=dependencies /lib /lib
COPY --from=dependencies /etc/ssl/ /etc/ssl
COPY --from=dependencies /tmp /tmp
COPY --from=dependencies /usr/local/bin /obmondo/bin
COPY --from=builder /app/kubeaid-bootstrap-script /obmondo/bin/kubeaid-bootstrap-script
CMD ["sleep", "infinity"]
