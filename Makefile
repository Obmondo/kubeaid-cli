# Needed for shell expansion
SHELL = /bin/bash

.PHONY: build-image-dev
build-image-dev:
	@docker build -f ./build/Dockerfile.dev --build-arg CPU_ARCHITECTURE=arm64 -t kubeaid-bootstrap-script-dev .

.PHONY: build-image
build-image:
	@docker build -f ./build/Dockerfile --build-arg CPU_ARCHITECTURE=arm64 -t kubeaid-bootstrap-script .

.PHONY: run-container-dev
run-container-dev:
	@docker run --name kubeaid-bootstrap-script-dev \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v .:/app \
		kubeaid-bootstrap-script-dev

.PHONY: run-container
run-container:
	@docker run --name kubeaid-bootstrap-script \
		-v /var/run/docker.sock:/var/run/docker.sock \
		kubeaid-bootstrap-script

.PHONY: exec-container-dev
exec-container-dev:
	@docker exec -it kubeaid-bootstrap-script-dev /bin/sh

.PHONY: exec-container
exec-container:
	@docker exec -it kubeaid-bootstrap-script /bin/sh

.PHONY: generate-sample-config-aws-dev
generate-sample-config-aws-dev:
	@go run ./cmd generate-sample-config \
    --cloud aws \
    --k8s-version v1.31.0

.PHONY: generate-sample-config-aws
generate-sample-config-aws:
	@kubeaid-bootstrap-script generate-sample-config \
    --cloud aws \
    --k8s-version v1.31.0

.PHONY: bootstrap-cluster-dev
bootstrap-cluster-dev:
	@go run ./cmd bootstrap-cluster --config-file /app/kubeaid-bootstrap-script.config.yaml

.PHONY: bootstrap-cluster
bootstrap-cluster:
	@kubeaid-bootstrap-script bootstrap-cluster --config-file /app/kubeaid-bootstrap-script.config.yaml

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
