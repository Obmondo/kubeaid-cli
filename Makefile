# Needed for shell expansion
SHELL = /bin/bash

.PHONY: build-image-dev
build-image-dev:
	@docker build -f ./build/Dockerfile.dev --build-arg CPU_ARCHITECTURE=arm64 -t kubeaid-bootstrap-script-dev .

.PHONY: run-container-dev
run-container-dev:
	@docker run --name kubeaid-bootstrap-script-dev \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v .:/app \
		kubeaid-bootstrap-script-dev

.PHONY: exec-container-dev
exec-container-dev:
	@docker exec -it kubeaid-bootstrap-script-dev /bin/sh

.PHONY: generate-sample-config-aws-dev
generate-sample-config-aws-dev:
	@go run ./cmd generate-sample-config \
    --cloud aws \
    --k8s-version v1.31.0

.PHONY: bootstrap-cluster-dev
bootstrap-cluster-dev:
	@go run ./cmd bootstrap-cluster \
		--config-file /app/outputs/kubeaid-bootstrap-script.config.yaml

.PHONY: use-management-cluster
use-management-cluster:
	export KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml

.PHONY: use-provisioned-cluster
use-provisioned-cluster:
	export KUBECONFIG=./outputs/provisioned-cluster.kubeconfig.yaml

.PHONY: delete-provisioned-cluster
delete-provisioned-cluster:
	KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml \
		kubectl delete clusters/kubeaid-demo -n capi-cluster

.PHONY: delete-management-cluster
delete-management-cluster:
	KUBECONFIG=./outputs/management-cluster.kubeconfig.yaml \
		k3d cluster delete management-cluster
