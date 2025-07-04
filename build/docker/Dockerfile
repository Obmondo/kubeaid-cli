# syntax=docker/dockerfile:1

#--- Builder stage ---

FROM golang:1.24.3 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download -x

COPY . .
RUN go build -v -o kubeaid-bootstrap-script ./cmd

#--- Packager stage ---

FROM golang:1.24.3 AS packager

# Set the maintainer label
LABEL org.opencontainers.image.authors="ashish@obmondo.com, archisman@obmondo.com"

WORKDIR /

COPY ./scripts/install-runtime-dependencies.sh /install-runtime-dependencies.sh
RUN chmod +x /install-runtime-dependencies.sh && \
  /install-runtime-dependencies.sh

COPY --from=builder /app/kubeaid-bootstrap-script /usr/local/bin/kubeaid-bootstrap-script

CMD ["sleep", "infinity"]
