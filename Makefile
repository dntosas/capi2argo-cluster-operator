SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
COMMIT = $(shell git log --pretty=format:'%h' -n 1)
VERSION=$(shell git describe --tags)
USER = $(shell id -u)
GROUP = $(shell id -g)
PROJECT = "capi2argo-cluster-operator"
GOBUILD_OPTS = -ldflags="-s -w -X ${PROJECT}/cmd.Version=${VERSION} -X ${PROJECT}/cmd.CommitHash=${COMMIT}"
GO_IMAGE = "golang:1.23-alpine"
GO_IMAGE_CI = "golangci/golangci-lint:v1.62.0"
DISTROLESS_IMAGE = "gcr.io/distroless/static:nonroot"
IMAGE_TAG_BASE ?= "ghcr.io/dntosas/${PROJECT}"

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint against code.
	golangci-lint run --timeout 5m --modules-download-mode=vendor --build-tags integration

.PHONY: test
test: envtest ## Run go tests against code.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test -v -mod=vendor `go list ./...` -coverprofile cover.out

.PHONY: ci
ci: fmt vet lint test ## Run go fmt/vet/lint/tests against the code.

.PHONY: modsync
modsync: ## Run go mod tidy && vendor.
	go mod tidy && go mod vendor

.PHONY: helm-docs
helm-docs:
	docker run --rm --volume "${PWD}/charts/capi2argo-cluster-operator:/helm-docs" -u ${USER} "jnorwood/helm-docs:v1.11.0"

##@ Build

.PHONY: build
build: ## Build capi-to-argocd-operator binary.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -mod=vendor ${GOBUILD_OPTS} -o ${PROJECT} main.go

.PHONY: build-darwin
build-darwin: ## Build capi-to-argocd-operator binary.
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -a -mod=vendor ${GOBUILD_OPTS} -o ${PROJECT} main.go

.PHONY: run
run: ## Run the controller from your host against your current kconfig context.
	go run -mod=vendor ./main.go

.PHONY: docker-build-dev
docker-build-dev: build ## Build docker image with the manager.
	docker build --build-arg GO_IMAGE=${GO_IMAGE} --build-arg DISTROLESS_IMAGE=${DISTROLESS_IMAGE} -t ${IMAGE_TAG_BASE}:dev --no-cache .

.PHONY: docker-push-dev
docker-push-dev: ## Push docker image with the manager.
	docker push ${IMAGE_TAG_BASE}:dev

.PHONY: helm-deploy-dev
helm-deploy-dev: ## Deploy helm chart with the manager.
	helm upgrade -i capi2argo charts/capi2argo-cluster-operator --set image.tag=dev

checksums:
	sha256sum bin/${PROJECT} > bin/${PROJECT}.sha256

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v4.5.7
CONTROLLER_TOOLS_VERSION ?= v0.16.5

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef