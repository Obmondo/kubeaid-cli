# Needed for shell expansion
SHELL = /bin/bash

.PHONY: format
format:
	@golangci-lint fmt

.PHONY: lint
lint:
	@golangci-lint run ./...

.PHONY: addlicense
addlicense:
	@find . -name '*.go' -exec addlicense -c "Obmondo" -l "AGPL3" -s {} +

.PHONY: run-generators
run-generators:
	@go run ./tools/generators/cmd \
    ./pkg/config/general.go ./pkg/config/secrets.go

VERSION := $(shell cat ./cmd/kubeaid-core/root/version/version.txt)
IMAGE_NAME=ghcr.io/obmondo/kubeaid-core:v$(VERSION)
CONTAINER_NAME=kubeaid-core

MANAGEMENT_CLUSTER_NAME=kubeaid-bootstrapper
NETWORK_NAME=k3d-$(MANAGEMENT_CLUSTER_NAME)

.PHONY: build-image
build-image:
	@docker build --build-arg CPU_ARCHITECTURE=arm64 -t $(IMAGE_NAME) .

.PHONY: remove-image
remove-image:
	@docker rmi $(IMAGE_NAME)

.PHONY: run-container
run-container: build-image-dev
	@if ! docker network ls | grep -q $(NETWORK_NAME); then \
		docker network create $(NETWORK_NAME); \
	fi
	@docker run --name $(CONTAINER_NAME) \
    --network $(NETWORK_NAME) \
    -v ./outputs:/outputs \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --rm \
    $(IMAGE_NAME)

.PHONY: exec-container
exec-container:
	@docker exec -it $(CONTAINER_NAME) /bin/sh

.PHONY: stop-container
stop-container:
	@docker stop $(CONTAINER_NAME)

.PHONY: remove-container
remove-container: stop-container-dev
	@docker rm $(CONTAINER_NAME)

.PHONY: sample-config-generate-aws
sample-config-generate-aws:
	@go run ./cmd/kubeaid-core config generate aws

.PHONY: devenv-create-aws
devenv-create-aws:
	@go run ./cmd/kubeaid-core devenv create \
		--debug \
    --configs-directory ./outputs/configs/aws/

.PHONY: bootstrap-cluster-aws
bootstrap-cluster-aws:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/aws/

.PHONY: upgrade-cluster-aws
upgrade-cluster-aws:
	@go run ./cmd/kubeaid-core cluster upgrade aws \
		--debug \
    --configs-directory ./outputs/configs/aws/ \
		--k8s-version "v1.32.0" --ami-id "ami-042e8a22a289729b1"

.PHONY: delete-provisioned-cluster-aws
delete-provisioned-cluster-aws:
	@go run ./cmd/kubeaid-core cluster delete \
    --configs-directory ./outputs/configs/aws/

.PHONY: recover-cluster-aws
recover-cluster-aws:
	@go run ./cmd/kubeaid-core cluster recover aws \
		--debug \
    --configs-directory ./outputs/configs/aws/ \
    --skip-pr-workflow

.PHONY: devenv-create-azure
devenv-create-azure:
	@go run ./cmd/kubeaid-core devenv create \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-pr-workflow
    
.PHONY: bootstrap-cluster-azure
bootstrap-cluster-azure:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-monitoring-setup \
    --skip-pr-workflow

.PHONY: upgrade-cluster-azure
upgrade-cluster-azure:
	@go run ./cmd/kubeaid-core cluster upgrade azure \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
		--k8s-version "v1.32.0"

.PHONY: delete-provisioned-cluster-azure
delete-provisioned-cluster-azure:
	@go run ./cmd/kubeaid-core cluster delete \
    --configs-directory ./outputs/configs/azure/

.PHONY: recover-cluster-azure
recover-cluster-azure:
	@go run ./cmd/kubeaid-core cluster recover azure \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-pr-workflow

.PHONY: devenv-create-hcloud
devenv-create-hcloud:
	@go run ./cmd/kubeaid-core devenv create \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hcloud \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-hcloud
bootstrap-cluster-hcloud:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hcloud/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: delete-provisioned-cluster-hcloud
delete-provisioned-cluster-hcloud:
	@go run ./cmd/kubeaid-core cluster delete \
    --configs-directory ./outputs/configs/hetzner/hcloud/

.PHONY: devenv-create-hetzner-bare-metal
devenv-create-hetzner-bare-metal:
	@go run ./cmd/kubeaid-core devenv create \
		--debug \
    --configs-directory ./outputs/configs/hetzner/bare-metal \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-hetzner-bare-metal
bootstrap-cluster-hetzner-bare-metal:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hetzner/bare-metal \
    --skip-pr-workflow

.PHONY: delete-provisioned-cluster-hetzner-bare-metal
delete-provisioned-cluster-hetzner-bare-metal:
	@go run ./cmd/kubeaid-core cluster delete \
    --debug \
    --configs-directory ./outputs/configs/hetzner/bare-metal/

.PHONY: devenv-create-hetzner-hybrid
devenv-create-hetzner-hybrid:
	@go run ./cmd/kubeaid-core devenv create \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hybrid \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-hetzner-hybrid
bootstrap-cluster-hetzner-hybrid:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hybrid/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: delete-provisioned-cluster-hetzner-hybrid
delete-provisioned-cluster-hetzner-hybrid:
	@go run ./cmd/kubeaid-core cluster delete \
    --debug \
    --configs-directory ./outputs/configs/hetzner/hybrid/

.PHONY: devenv-create-bare-metal
devenv-create-bare-metal:
	@go run ./cmd/kubeaid-core devenv create \
		--debug \
    --configs-directory ./outputs/configs/bare-metal/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-bare-metal
bootstrap-cluster-bare-metal:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/bare-metal/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: test-cluster-bare-metal
test-cluster-bare-metal:
	@go run ./cmd/kubeaid-core cluster test \
		--debug \
    --configs-directory ./outputs/configs/bare-metal/

.PHONY: delete-provisioned-cluster-bare-metal
delete-provisioned-cluster-bare-metal:
	@go run ./cmd/kubeaid-core cluster delete \
    --debug \
    --configs-directory ./outputs/configs/bare-metal/

.PHONY: bootstrap-cluster-local
bootstrap-cluster-local:
	@go run ./cmd/kubeaid-core cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/local/ \
    --skip-monitoring-setup \
    --skip-pr-workflow

.PHONY: management-cluster-delete
management-cluster-delete:
	KUBECONFIG=./outputs/kubeconfigs/clusters/management/container.yaml \
		k3d cluster delete $(MANAGEMENT_CLUSTER_NAME)
