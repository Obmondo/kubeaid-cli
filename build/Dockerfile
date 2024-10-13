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

ARG CPU_ARCHITECTURE
ENV CPU_ARCHITECTURE=${CPU_ARCHITECTURE}

WORKDIR /

COPY ./scripts/install-prerequisites.sh /install-prerequisites.sh
RUN sh /install-prerequisites.sh

COPY --from=builder /app/kubeaid-bootstrap-script /usr/local/bin/kubeaid-bootstrap-script

CMD sleep infinity
