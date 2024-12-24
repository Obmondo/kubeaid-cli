# Needed for shell expansion
SHELL = /bin/bash
CURRENT_DIR := $(CURDIR)
CONTAINER_NAME=kubeaid-bootstrap-script-dev
NETWORK_NAME=k3d-management-cluster
IMAGE_NAME=kubeaid-bootstrap-script-dev:latest

.PHONY: build-image-dev
build-image-dev:
	@docker build -f ./build/Dockerfile.dev --build-arg CPU_ARCHITECTURE=arm64 -t $(IMAGE_NAME) .

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

.PHONY: generate-sample-config-aws-dev
generate-sample-config-aws-dev:
	@go run ./cmd config generate aws

.PHONY: bootstrap-cluster-dev-aws
bootstrap-cluster-dev-aws:
	@go run ./cmd cluster bootstrap aws \
		--debug \
		--config /app/outputs/kubeaid-bootstrap-script.config.yaml \
		--skip-clusterctl-move
# --skip-kubeaid-config-setup

.PHONY: bootstrap-cluster-dev-hetzner
bootstrap-cluster-dev-hetzner:
	@go run ./cmd cluster bootstrap hetzner \
		--debug \
		--config /app/outputs/kubeaid-bootstrap-script.hetzner.config.yaml
# --skip-kubeaid-config-setup
# --skip-clusterctl-move

.PHONY: use-management-cluster
use-management-cluster:
	export KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml

.PHONY: use-provisioned-cluster
use-provisioned-cluster:
	export KUBECONFIG=./outputs/provisioned-cluster.kubeconfig.yaml

.PHONY: delete-provisioned-cluster-dev
delete-provisioned-cluster-dev:
	@go run ./cmd cluster delete \
		--config /app/outputs/kubeaid-bootstrap-script.hetzner.config.yaml

.PHONY: delete-management-cluster
delete-management-cluster:
	KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml \
		k3d cluster delete management-cluster
