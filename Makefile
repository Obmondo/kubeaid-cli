VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version.Version=$(VERSION) \
	-X github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version.Commit=$(COMMIT) \
	-X github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version.Date=$(BUILD_DATE)

IMAGE_NAME := ghcr.io/obmondo/kubeaid-core:$(VERSION)
CONTAINER_NAME := kubeaid-core
MANAGEMENT_CLUSTER_NAME := kubeaid-bootstrapper
NETWORK_NAME := k3d-$(MANAGEMENT_CLUSTER_NAME)

default: help ## Run help by default

help: ## This help screen
	@printf "Available targets:\n\n"
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9][a-zA-Z0-9_-]*:/ { \
		target = $$1; \
		sub(/:.*/, "", target); \
		rest = substr($$0, index($$0, ":") + 1); \
		while (substr(rest, 1, 1) == " " || substr(rest, 1, 1) == "\t") { \
			rest = substr(rest, 2); \
		} \
		if (substr(rest, 1, 1) == "=") { \
			next; \
		} \
		description = "No description"; \
		if (index($$0, "## ") > 0) { \
			description = $$2; \
		} \
		printf "  \x1b[32;01m%-35s\x1b[0m %s\n", target, description; \
	}' $(MAKEFILE_LIST) | sort -u
	@printf "\n"


.PHONY: format
format: ## Run formatter checks
	@golangci-lint fmt

.PHONY: lint
lint: ## Run Go linters
	@golangci-lint run ./...

.PHONY: addlicense
addlicense: ## Add AGPL3 headers to Go files
	@find . -name '*.go' -exec addlicense -c "Obmondo" -l "AGPL3" -s {} +

.PHONY: test
test: ## Run unit tests and write coverage.out
	@go test -count=1 -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: coverage
coverage: test ## Open the per-file HTML coverage report in a browser
	@go tool cover -html=coverage.out

.PHONY: check-coverage
check-coverage: test ## Enforce testcoverage.yaml thresholds
	@go run github.com/vladopajic/go-test-coverage/v2@latest --config=./testcoverage.yaml

.PHONY: run-generators
run-generators: ## Generate config artifacts
	@go run ./tools/generators/cmd \
		./pkg/config/general.go ./pkg/config/secrets.go

.PHONY: build-kubeaid-core
build-kubeaid-core: ## Build kubeaid-core binary
	@go build -ldflags="$(LDFLAGS)" -o ./build/kubeaid-core ./cmd/kubeaid-core

.PHONY: build-kubeaid-storagectl
build-kubeaid-storagectl: ## Build kubeaid-storagectl binary
	@go build -ldflags="$(LDFLAGS)" -o ./build/kubeaid-storagectl ./cmd/kubeaid-storagectl

.PHONY: build-cli
build-cli: ## Build kubeaid-cli binary
	@CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o ./build/kubeaid-cli ./cmd/kubeaid-cli

.PHONY: build-image
build-image: ## Build container image
	@docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE_NAME) .

.PHONY: remove-image
remove-image: ## Remove container image
	@docker rmi $(IMAGE_NAME)

.PHONY: run-container
run-container: build-image ## Run container with local mounts
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
exec-container: ## Open shell in running container
	@docker exec -it $(CONTAINER_NAME) /bin/sh

.PHONY: stop-container
stop-container: ## Stop running container
	@docker stop $(CONTAINER_NAME)

.PHONY: remove-container
remove-container: stop-container ## Stop and remove container
	@docker rm $(CONTAINER_NAME)

.PHONY: management-cluster-delete
management-cluster-delete: ## Delete the management k3d cluster
	KUBECONFIG=./outputs/kubeconfigs/clusters/management/container.yaml \
		k3d cluster delete $(MANAGEMENT_CLUSTER_NAME)
