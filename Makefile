# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests.
	go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= kubernetes-aggregator-framework-test-e2e

.PHONY: setup-test-integration
setup-test-integration: cleanup-test-integration ## Set up a Kind cluster for integration tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please execute make deps."; \
		exit 1; \
	}

	$(KIND) create cluster --name $(KIND_CLUSTER)

	$(KUBECTL) wait --for=condition=Ready node/$(KIND_CLUSTER)-control-plane --timeout=120s

.PHONY: test-integration
test-integration: chainsaw setup-test-integration _test-integration

_test-integration:
	$(CHAINSAW) test --test-dir test/integration/00-dependencies
	$(MAKE) cleanup-test-integration

.PHONY: cleanup-test-integration
cleanup-test-integration: ## Tear down the Kind cluster used for integration tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= $(LOCALBIN)/kubectl
KIND ?= $(LOCALBIN)/kind
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
VCLUSTER ?= $(LOCALBIN)/vcluster
CHAINSAW ?= $(LOCALBIN)/chainsaw

## Tool Versions
KUBE_VERSION ?= v1.34.0
KIND_VERSION ?= v0.30.0
GOLANGCI_LINT_VERSION ?= v2.1.0
VCLUSTER_VERSION ?= v0.30.0
CHAINSAW_VERSION ?= v0.2.12

.PHONY: chainsaw
chainsaw: $(CHAINSAW) ## Download chainsaw locally if necessary.
$(CHAINSAW): $(LOCALBIN)
	$(call go-install-tool,$(CHAINSAW),github.com/kyverno/chainsaw,$(CHAINSAW_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

deps: $(LOCALBIN) chainsaw golangci-lint
	curl -Lo $(KIND) https://kind.sigs.k8s.io/dl/$(KIND_VERSION)/kind-linux-amd64
	chmod +x $(KIND)

	curl -Lo $(KUBECTL) https://dl.k8s.io/release/$(KUBE_VERSION)/bin/linux/amd64/kubectl
	chmod +x $(KUBECTL)

	curl -Lo $(VCLUSTER) https://github.com/loft-sh/vcluster/releases/download/$(VCLUSTER_VERSION)/vcluster-linux-amd64
	chmod +x $(VCLUSTER)
