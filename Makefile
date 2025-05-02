# Needed for shell expansion
SHELL = /bin/bash
CURRENT_DIR := $(CURDIR)
CONTAINER_NAME=kubeaid-bootstrap-script-dev
NETWORK_NAME=k3d-management-cluster
IMAGE_NAME=kubeaid-bootstrap-script-dev:latest

.PHONY: build-image-dev
build-image-dev:
	@docker build -f ./build/docker/Dockerfile.dev --build-arg CPU_ARCHITECTURE=arm64 -t $(IMAGE_NAME) .

.PHONY: remove-image-dev
remove-image-dev:
	@docker rmi $(IMAGE_NAME)

.PHONY: run-container-dev
run-container-dev: build-image-dev
	@if ! docker network ls | grep -q $(NETWORK_NAME); then \
		docker network create $(NETWORK_NAME); \
	fi
	@docker run --name $(CONTAINER_NAME) \
    --network $(NETWORK_NAME) \
    --detach \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v $(CURRENT_DIR):/app \
    $(IMAGE_NAME)

# -e SSH_AUTH_SOCK=/ssh-agent \
# -v /dev/bus/usb:/dev/bus/usb \
# -v $(SSH_AUTH_SOCK):/ssh-agent \

.PHONY: exec-container-dev
exec-container-dev:
	@docker exec -it $(CONTAINER_NAME) /bin/sh

.PHONY: stop-container-dev
stop-container-dev:
	@docker stop $(CONTAINER_NAME)

.PHONY: remove-container-dev
remove-container-dev: stop-container-dev
	@docker rm $(CONTAINER_NAME)

.PHONY: sample-config-generate-aws-dev
sample-config-generate-aws-dev:
	@go run ./cmd/ config generate aws

.PHONY: devenv-create-aws-dev
devenv-create-aws-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/aws/

.PHONY: devenv-create-azure-dev
devenv-create-azure-dev:
	@go run ./cmd/ devenv create \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-pr-flow \
    --skip-kube-prometheus-build

.PHONY: bootstrap-cluster-aws-dev
bootstrap-cluster-aws-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/aws/
# --skip-kube-prometheus-build
# --skip-clusterctl-move

.PHONY: bootstrap-cluster-azure-dev
bootstrap-cluster-azure-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
    --skip-monitoring-setup \
    --skip-pr-flow \
    --skip-kube-prometheus-build

.PHONY: bootstrap-cluster-hetzner-dev
bootstrap-cluster-hetzner-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/hcloud/ \
    --skip-kube-prometheus-build

.PHONY: upgrade-cluster-aws-dev
upgrade-cluster-aws-dev:
	@go run ./cmd/ cluster upgrade aws \
		--debug \
    --configs-directory ./outputs/configs/aws/ \
		--k8s-version "v1.32.0" --ami-id "ami-042e8a22a289729b1"

.PHONY: upgrade-cluster-azure-dev
upgrade-cluster-azure-dev:
	@go run ./cmd/ cluster upgrade azure \
		--debug \
    --configs-directory ./outputs/configs/azure/ \
		--k8s-version "v1.32.0"

.PHONY: delete-provisioned-cluster-aws-dev
delete-provisioned-cluster-aws-dev:
	@go run ./cmd/ cluster delete \
    --configs-directory ./outputs/configs/aws/

.PHONY: delete-provisioned-cluster-azure-dev
delete-provisioned-cluster-azure-dev:
	@go run ./cmd/ cluster delete \
    --configs-directory ./outputs/configs/azure/

.PHONY: delete-provisioned-cluster-hetzner-dev
delete-provisioned-cluster-hetzner-dev:
	@go run ./cmd/ cluster delete \
    --configs-directory ./outputs/configs/hcloud/

.PHONY: bootstrap-cluster-local-dev
bootstrap-cluster-local-dev:
	@go run ./cmd/ cluster bootstrap \
		--debug \
    --configs-directory ./outputs/configs/local/ \
    --skip-monitoring-setup \
    --skip-kube-prometheus-build \
    --skip-pr-flow

.PHONY: management-cluster-delete
management-cluster-delete:
	KUBECONFIG=./outputs/kubeconfigs/clusters/management/container.yaml \
		k3d cluster delete management-cluster
