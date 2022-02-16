SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
COMMIT = $(shell git log --pretty=format:'%h' -n 1)
VERSION = "v0.1.0"
# VERSION=$(shell git describe --tags)
PROJECT = "capi2argo-cluster-operator"
GOBUILD_OPTS = -ldflags="-s -w -X ${PROJECT}/cmd.Version=${VERSION} -X ${PROJECT}/cmd.CommitHash=${COMMIT}"
GO_IMAGE = "golang:1.17"
GO_IMAGE_CI = "golangci/golangci-lint:v1.44.0"
DISTROLESS_IMAGE = "gcr.io/distroless/static:nonroot"
IMAGE_TAG_BASE ?= dntosas/${PROJECT}

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

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
	golangci-lint run --enable revive,bodyclose,gofmt,exportloopref --exclude-use-default=false --modules-download-mode=vendor --build-tags integration

.PHONY: test
test: ## Run go tests against code.
	go test -v -mod=vendor `go list ./...` -coverprofile cover.out

.PHONY: ci
ci: fmt vet lint test ## Run go fmt/vet/lint/tests against the code.

.PHONY: modsync
modsync: ## Run go mod tidy && vendor.
	go mod tidy && go mod vendor

##@ Build

.PHONY: build
build: fmt vet ## Build capi-to-argocd-operator binary.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -mod=vendor ${GOBUILD_OPTS} -o bin/${PROJECT} main.go

.PHONY: run
run: ## Run the controller from your host against your current kconfig context.
	go run -mod=vendor ./main.go

.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	docker build --build-arg GO_IMAGE=${GO_IMAGE} --build-arg DISTROLESS_IMAGE=${DISTROLESS_IMAGE} -t ${IMAGE_TAG_BASE}:${VERSION} --no-cache .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMAGE_TAG_BASE}:${VERSION}

checksums:
	sha256sum ${PROJECT} > ${PROJECT}.sha256

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

ENVTEST = $(shell pwd)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

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