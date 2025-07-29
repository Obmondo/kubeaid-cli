# Needed for shell expansion
SHELL = /bin/bash
CURRENT_DIR := $(CURDIR)
MANAGEMENT_CLUSTER_NAME=kubeaid-bootstrapper

.PHONY: lint
lint:
	@golangci-lint run ./...

.PHONY: build
build:
	@go build -o build/kubeaid-bootstrap-script ./cmd

.PHONY: sample-config-generate-aws-dev
sample-config-generate-aws-dev:
	@go run ./cmd/ config generate aws

.PHONY: devenv-create-aws-dev
devenv-create-aws-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/aws/

.PHONY: bootstrap-cluster-aws-dev
bootstrap-cluster-aws-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/aws/

.PHONY: upgrade-cluster-aws-dev
upgrade-cluster-aws-dev:
	@go run ./cmd/ cluster upgrade aws \
		--debug \
    --configs-directory ./outputs/configs/aws/ \
		--k8s-version "v1.32.0" --ami-id "ami-042e8a22a289729b1"

.PHONY: delete-provisioned-cluster-aws-dev
delete-provisioned-cluster-aws-dev:
	@go run ./cmd/ cluster delete \
    --configs-directory ./outputs/configs/aws/

.PHONY: recover-cluster-aws-dev
recover-cluster-aws-dev:
	@go run ./cmd/ cluster recover aws \
		--debug \
    --configs-directory ./outputs/configs/aws/ \
    --skip-pr-workflow

.PHONY: sample-config-generate-azure-dev
sample-config-generate-azure-dev:
	@go run ./cmd/ config generate azure

.PHONY: devenv-create-azure-dev
devenv-create-azure-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-pr-workflow
    
.PHONY: bootstrap-cluster-azure-dev
bootstrap-cluster-azure-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-monitoring-setup \
    --skip-pr-workflow

.PHONY: upgrade-cluster-azure-dev
upgrade-cluster-azure-dev:
	@go run ./cmd/ cluster upgrade azure \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
		--k8s-version "v1.32.0"

.PHONY: delete-provisioned-cluster-azure-dev
delete-provisioned-cluster-azure-dev:
	@go run ./cmd/ cluster delete \
    --configs-directory ./outputs/configs/azure/

.PHONY: recover-cluster-azure-dev
recover-cluster-azure-dev:
	@go run ./cmd/ cluster recover azure \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-pr-workflow

.PHONY: sample-config-generate-hcloud-dev
sample-config-generate-hcloud-dev:
	@go run ./cmd/ config generate hetzner hcloud

.PHONY: devenv-create-hcloud-dev
devenv-create-hcloud-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hcloud \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-hcloud-dev
bootstrap-cluster-hcloud-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hcloud/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: delete-provisioned-cluster-hcloud-dev
delete-provisioned-cluster-hcloud-dev:
	@go run ./cmd/ cluster delete \
    --configs-directory ./outputs/configs/hetzner/hcloud/

.PHONY: sample-config-generate-hcloud-dev
sample-config-generate-hetzner-bare-metal-dev:
	@go run ./cmd/ config generate hetzner bare-metal

.PHONY: devenv-create-hetzner-bare-metal-dev
devenv-create-hetzner-bare-metal-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/hetzner/bare-metal \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-hetzner-bare-metal-dev
bootstrap-cluster-hetzner-bare-metal-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hetzner/bare-metal \
    --skip-pr-workflow

.PHONY: delete-provisioned-cluster-hetzner-bare-metal-dev
delete-provisioned-cluster-hetzner-bare-metal-dev:
	@go run ./cmd/ cluster delete \
    --debug \
    --configs-directory ./outputs/configs/hetzner/bare-metal/

.PHONY: sample-config-generate-hetzner-hybrid-dev
sample-config-generate-hetzner-hybrid-dev:
	@go run ./cmd/ config generate hetzner hybrid

.PHONY: devenv-create-hetzner-hybrid-dev
devenv-create-hetzner-hybrid-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hybrid \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-hetzner-hybrid-dev
bootstrap-cluster-hetzner-hybrid-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hetzner/hybrid/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: delete-provisioned-cluster-hetzner-hybrid-dev
delete-provisioned-cluster-hetzner-hybrid-dev:
	@go run ./cmd/ cluster delete \
    --debug \
    --configs-directory ./outputs/configs/hetzner/hybrid/

.PHONY: sample-config-generate-bare-metal-dev
sample-config-generate-bare-metal-dev:
	@go run ./cmd/ config generate bare-metal

.PHONY: devenv-create-bare-metal-dev
devenv-create-bare-metal-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/bare-metal/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: bootstrap-cluster-bare-metal-dev
bootstrap-cluster-bare-metal-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/bare-metal/ \
    --skip-pr-workflow \
    --skip-monitoring-setup

.PHONY: test-cluster-bare-metal-dev
test-cluster-bare-metal-dev:
	@go run ./cmd/ cluster test \
		--debug \
    --configs-directory ./outputs/configs/bare-metal/

.PHONY: delete-provisioned-cluster-bare-metal-dev
delete-provisioned-cluster-bare-metal-dev:
	@go run ./cmd/ cluster delete \
    --debug \
    --configs-directory ./outputs/configs/bare-metal/

.PHONY: sample-config-generate-local-dev
sample-config-generate-local-dev:
	@go run ./cmd/ config generate local

.PHONY: bootstrap-cluster-local-dev
bootstrap-cluster-local-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/local/ \
    --skip-monitoring-setup \
    --skip-pr-workflow

.PHONY: management-cluster-delete
management-cluster-delete:
	KUBECONFIG=./outputs/kubeconfigs/clusters/management/container.yaml \
		k3d cluster delete $(MANAGEMENT_CLUSTER_NAME)
