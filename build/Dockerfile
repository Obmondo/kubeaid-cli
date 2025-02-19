# syntax=docker/dockerfile:1

#--- Builder stage ---

FROM golang:1.23.0 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o kubeaid-bootstrap-script ./cmd

#--- Packager stage ---

FROM golang:1.23.0 AS packages

# Set the maintainer label
LABEL org.opencontainers.image.authors="ashish@obmondo.com, archisman@obmondo.com"

WORKDIR /

COPY ./scripts/install-prerequisites.sh /install-prerequisites.sh
RUN chmod +x /install-prerequisites.sh
RUN CPU_ARCHITECTURE=$([ "$(uname -m)" = "x86_64" ] && echo "amd64" || echo "arm64") \
  /install-prerequisites.sh

COPY --from=builder /app/kubeaid-bootstrap-script /usr/local/bin/kubeaid-bootstrap-script

CMD ["sleep", "infinity"]
