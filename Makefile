VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version.Version=$(VERSION) \
	-X github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version.Commit=$(COMMIT) \
	-X github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-core/root/version.Date=$(BUILD_DATE)

MANAGEMENT_CLUSTER_NAME := kubeaid-bootstrapper

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

.PHONY: fetch-k8s-eol
fetch-k8s-eol: ## Refresh embedded K8s EOL data from endoflife.date
	@./tools/generators/cmd/fetch-k8s-eol.sh

.PHONY: check-k8s-eol
check-k8s-eol: ## Check that pkg/config/parser/k8s-eol.json is up to date with endoflife.date
	@./tools/generators/cmd/fetch-k8s-eol.sh
	@if ! git diff --quiet -- pkg/config/parser/k8s-eol.json; then \
		echo ""; \
		echo "pkg/config/parser/k8s-eol.json is out of date relative to endoflife.date."; \
		echo "Run 'make fetch-k8s-eol' locally and commit the refreshed file."; \
		echo ""; \
		git --no-pager diff -- pkg/config/parser/k8s-eol.json; \
		exit 1; \
	fi
	@echo "pkg/config/parser/k8s-eol.json is up to date with endoflife.date"

.PHONY: build
build: ## Build kubeaid-cli binary
	@CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o ./build/kubeaid-cli ./cmd/kubeaid-cli

.PHONY: build-storagectl
build-storagectl: ## Build kubeaid-storagectl binary
	@go build -ldflags="$(LDFLAGS)" -o ./build/kubeaid-storagectl ./cmd/kubeaid-storagectl

.PHONY: management-cluster-delete
management-cluster-delete: ## Delete the management k3d cluster
	KUBECONFIG=./outputs/kubeconfigs/clusters/management/container.yaml \
		k3d cluster delete $(MANAGEMENT_CLUSTER_NAME)
