# Needed for shell expansion
SHELL = /bin/bash
CURRENT_DIR := $(CURDIR)
CONTAINER_NAME=kubeaid-bootstrap-script-dev
NETWORK_NAME=k3d-management-cluster
IMAGE_NAME=kubeaid-bootstrap-script-dev:latest

.PHONY: build-image-dev
build-image-dev:
	@docker build -f ./build/kubeaid-bootstrap-script/Dockerfile.dev --build-arg CPU_ARCHITECTURE=arm64 -t $(IMAGE_NAME) .

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
	@go run ./cmd/kubeaid-bootstrap-script/ config generate aws

.PHONY: devenv-create-aws-dev
devenv-create-aws-dev:
	@go run ./cmd/kubeaid-bootstrap-script/ devenv create aws \
		--debug \
		--config ./outputs/kubeaid-bootstrap-script.aws.config.yaml

.PHONY: bootstrap-cluster-aws-dev
bootstrap-cluster-aws-dev:
	@go run ./cmd/kubeaid-bootstrap-script/ cluster bootstrap aws \
		--debug \
		--config ./outputs/kubeaid-bootstrap-script.aws.config.yaml \
    --skip-kube-prometheus-build
# --skip-clusterctl-move

.PHONY: bootstrap-cluster-hetzner-dev
bootstrap-cluster-hetzner-dev:
	@go run ./cmd/kubeaid-bootstrap-script/ cluster bootstrap hetzner \
		--debug \
    --config ./outputs/kubeaid-bootstrap-script.hetzner.config.yaml \
    --skip-clusterctl-move
# --skip-kube-prometheus-build

.PHONY: use-management-cluster
use-management-cluster:
	export KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml

.PHONY: use-provisioned-cluster
use-provisioned-cluster:
	export KUBECONFIG=./outputs/provisioned-cluster.kubeconfig.yaml

.PHONY: provisioned-cluster-delete-aws-dev
provisioned-cluster-delete-aws-dev:
	@go run ./cmd/kubeaid-bootstrap-script/ cluster delete \
		--config ./outputs/kubeaid-bootstrap-script.aws.config.yaml

.PHONY: provisioned-cluster-delete-hetzner-dev
provisioned-cluster-delete-hetzner-dev:
	@go run ./cmd/kubeaid-bootstrap-script/ cluster delete \
		--config ./outputs/kubeaid-bootstrap-script.hetzner.config.yaml

.PHONY: management-cluster-delete
management-cluster-delete:
	KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml \
		k3d cluster delete management-cluster
