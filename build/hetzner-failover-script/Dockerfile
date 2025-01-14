# syntax=docker/dockerfile:1

#--- Builder stage ---

FROM golang:1.23.0 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o hetzner-failover-script ./cmd/hetzner-failover-script

#--- Packager stage ---

FROM golang:1.23.0 AS packages

# Set the maintainer label
LABEL org.opencontainers.image.authors="ashish@obmondo.com, archisman@obmondo.com"

RUN apk add --no-cache procps

WORKDIR /root/

COPY --from=builder /app/hetzner-failover-script .

CMD ["./hetzner-failover-script"]
