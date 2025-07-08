.DEFAULT_GOAL=all

# Image URL to use all building/pushing image targets
IMG ?= fdb-kubernetes-operator:latest
SIDECAR_IMG ?=
REMOTE_BUILD ?= 0
CRD_OPTIONS ?= "crd:maxDescLen=0,crdVersions=v1,generateEmbeddedObjectMeta=true"

ifneq "$(FDB_WEBSITE)" ""
	img_build_args := $(img_build_args) --build-arg FDB_WEBSITE=$(FDB_WEBSITE)
endif

# Support overriding the default build platform
ifneq "$(BUILD_PLATFORM)" ""
	img_build_args := $(img_build_args) --platform $(BUILD_PLATFORM)
endif

# TAG is used to define the version in the kubectl-fdb plugin.
# If not defined we use the current git hash.
ifndef TAG
	TAG := $(shell git rev-parse HEAD)
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Dependencies to fetch through `go`
CONTROLLER_GEN_PKG?=sigs.k8s.io/controller-tools/cmd/controller-gen@v0.18.0
CONTROLLER_GEN=$(GOBIN)/controller-gen
KUSTOMIZE_PKG?=sigs.k8s.io/kustomize/kustomize/v4@v4.5.2
KUSTOMIZE=$(GOBIN)/kustomize
GOLANGCI_LINT_PKG=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6
GOLANGCI_LINT=$(GOBIN)/golangci-lint
GORELEASER_PKG=github.com/goreleaser/goreleaser/v2@v2.10.2
GORELEASER=$(GOBIN)/goreleaser
GO_LINES_PKG=github.com/segmentio/golines@v0.12.2
GO_LINES=$(GOBIN)/golines
GO_IMPORTS_PKG=golang.org/x/tools/cmd/goimports@v0.34.0
GO_IMPORTS=$(GOBIN)/goimports

BUILD_DEPS?=
BUILDER?="docker"
BUILDER_ARGS?=
KUBECTL_ARGS?=

define godep
BUILD_DEPS+=$(1)
$($2):
	(export TMP_DIR=$$$$(mktemp -d) ;\
	cd $$$$TMP_DIR ;\
	go install $$($2_PKG) ;\
	cd - ;\
	rm -rf $$$$TMP_DIR )
$(1): $$($2)
endef

$(eval $(call godep,controller-gen,CONTROLLER_GEN))
$(eval $(call godep,golangci-lint,GOLANGCI_LINT))
$(eval $(call godep,kustomize,KUSTOMIZE))
$(eval $(call godep,goreleaser,GORELEASER))
$(eval $(call godep,golines,GO_LINES))
$(eval $(call godep,goimports,GO_IMPORTS))

GO_SRC=$(shell find . -name "*.go" -not -name "zz_generated.*.go" -not -name ".\#*.go")
GENERATED_GO=api/v1beta2/zz_generated.deepcopy.go
GO_ALL=${GO_SRC} ${GENERATED_GO}
MANIFESTS=config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml
SAMPLES=config/samples/deployment.yaml config/samples/cluster.yaml config/samples/backup.yaml config/samples/restore.yaml config/samples/client.yaml

ifeq "$(TEST_RACE_CONDITIONS)" "1"
	go_test_flags := $(go_test_flags) -race -timeout=90m
endif

all: deps generate fmt vet manager snapshot manifests samples documentation test_if_changed

.PHONY: clean all manager samples documentation run install uninstall deploy manifests fmt vet generate container-build container-push container-push-if-remote rebuild-operator bounce lint

deps: $(BUILD_DEPS)

clean:
	find config/crd/bases -type f -name "*.yaml" -delete
	find api -type f -name "zz_generated.*.go" -delete
	mkdir -p bin
	rm -r bin
	rm -f $(SAMPLES)
	rm -f config/rbac/role.yaml
	rm -f config/development/kustomization.yaml
	rm -rf ./dist/*
	find . -name "cover.out" -delete

clean-deps:
	@rm $(CONTROLLER_GEN) $(KUSTOMIZE) $(GOLANGCI_LINT) $(GORELEASER) $(GO_LINES) $(GO_IMPORTS)

test_if_changed: cover.out

# Run tests
cover.out: ${GO_ALL} ${MANIFESTS}

test:
ifneq "$(SKIP_TEST)" "1"
	go test ${go_test_flags} ./... -coverprofile cover.out -ginkgo.timeout=2h -ginkgo.label-filter="!e2e"
endif

# Build manager binary
manager: bin/manager

bin/manager: ${GO_SRC}
	go build -ldflags="-s -w -X github.com/FoundationDB/fdb-kubernetes-operator/v2/setup.operatorVersion=${TAG}" -o bin/manager main.go

plugin-go:
	go build -ldflags="-s -w -X github.com/FoundationDB/fdb-kubernetes-operator/v2/kubectl-fdb/cmd.pluginVersion=${TAG}" -o bin/kubectl-fdb ./kubectl-fdb/main.go

# Build kubectl-fdb binary
plugin: bin/kubectl-fdb

bin/kubectl-fdb: ${GO_SRC} $(GORELEASER)
	$(GORELEASER) build --single-target --skip validate --clean
	@mkdir -p bin
	@touch $@

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate manifests
	go run ./main.go

# Install CRDs into a cluster
install: $(KUSTOMIZE) manifests
	$(KUSTOMIZE) build config/crd | kubectl $(KUBECTL_ARGS) apply -f -

# Uninstall CRDs from a cluster
uninstall: $(KUSTOMIZE) manifests
	$(KUSTOMIZE) build config/crd | kubectl $(KUBECTL_ARGS) delete -f -

# Apply config to the local development environment based on environment
# variables.
config/development/kustomization.yaml: $(KUSTOMIZE) config/development/kustomization.yaml.sample
	cp config/development/kustomization.yaml.sample config/development/kustomization.yaml
	cd config/development && $(KUSTOMIZE) edit set image foundationdb/fdb-kubernetes-operator=${IMG}
ifneq "$(SIDECAR_IMG)" ""
	cd config/development && $(KUSTOMIZE) edit set image foundationdb/foundationdb-kubernetes-sidecar=${SIDECAR_IMG}
endif
ifeq "$(REMOTE_BUILD)" "1"
	cd config/development && $(KUSTOMIZE) edit add patch --path=remote_build.yaml
endif

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: $(KUSTOMIZE) install manifests config/development/kustomization.yaml
	$(KUSTOMIZE) build config/development | kubectl $(KUBECTL_ARGS) apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: ${MANIFESTS}

${MANIFESTS}: ${CONTROLLER_GEN} ${GO_SRC}
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./api/..." paths="./controllers/..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt: deps bin/fmt_check

bin/fmt_check: ${GO_ALL}
	# We make use of gofmt with golines because of: https://github.com/segmentio/golines/issues/155
	@$(GO_LINES) -w --base-formatter=gofmt --ignored-dirs=./e2e/chaos-mesh --ignore-generated $(GO_SRC)
	@$(GO_IMPORTS) -w $(GO_SRC)
	@$(GOLANGCI_LINT) run --fix
	@mkdir -p bin
	@touch $@

# Run go vet against code
vet: bin/vet_check

bin/vet_check: ${GO_ALL}
	@go vet ./...
	@mkdir -p bin
	@touch $@

# Generate code
generate: ${GENERATED_GO}

${GENERATED_GO}: ${GO_SRC} hack/boilerplate.go.txt ${CONTROLLER_GEN}
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./..."

# Build the container image
container-build:
	$(BUILDER) build --build-arg=TAG=${TAG} ${img_build_args} $(BUILDER_ARGS) -t ${IMG} .

# Push the container image
container-push:
	$(BUILDER) push ${IMG}

# Push the container image
container-push-if-remote:
ifeq "$(REMOTE_BUILD)" "1"
	$(BUILDER) push ${IMG}
endif

# Rebuilds, deploys, and bounces the operator
rebuild-operator: container-build container-push-if-remote deploy bounce

bounce:
	kubectl $(KUBECTL_ARGS) delete pod -l app=fdb-kubernetes-operator-controller-manager

samples: $(KUSTOMIZE) ${SAMPLES}

config/samples/deployment.yaml: $(KUSTOMIZE) config/deployment/*.yaml
	$(KUSTOMIZE) build ./config/deployment > $@

config/samples/cluster.yaml: $(KUSTOMIZE) config/tests/base/*.yaml
	$(KUSTOMIZE) build ./config/tests/base/ > $@

config/samples/backup.yaml: $(KUSTOMIZE) config/tests/backup/base/*.yaml
	$(KUSTOMIZE) build ./config/tests/backup/base > $@

config/samples/restore.yaml: $(KUSTOMIZE) config/tests/backup/base/*.yaml
	$(KUSTOMIZE) build ./config/tests/backup/restore > $@

config/samples/client.yaml: $(KUSTOMIZE) config/tests/client/*.yaml
	$(KUSTOMIZE) build ./config/tests/client > $@

bin/po-docgen: cmd/po-docgen/*.go
	go build -o bin/po-docgen cmd/po-docgen/main.go  cmd/po-docgen/api.go

CLUSTER_DOCS_INPUT=api/v1beta2/foundationdbcluster_types.go api/v1beta2/foundationdb_custom_parameter.go api/v1beta2/foundationdb_database_configuration.go api/v1beta2/foundationdb_process_class.go api/v1beta2/image_config.go

docs/cluster_spec.md: bin/po-docgen $(CLUSTER_DOCS_INPUT)
	bin/po-docgen api $(CLUSTER_DOCS_INPUT) > $@

docs/backup_spec.md: bin/po-docgen api/v1beta2/foundationdbbackup_types.go
	bin/po-docgen api api/v1beta2/foundationdbbackup_types.go api/v1beta2/foundationdb_custom_parameter.go api/v1beta2/image_config.go > $@

docs/restore_spec.md: bin/po-docgen api/v1beta2/foundationdbrestore_types.go
	bin/po-docgen api api/v1beta2/foundationdbrestore_types.go api/v1beta2/foundationdb_custom_parameter.go > $@

documentation: docs/cluster_spec.md docs/backup_spec.md docs/restore_spec.md

lint: bin/lint

bin/lint: $(GOLANGCI_LINT) ${GO_SRC}
	$(GOLANGCI_LINT) run ./...
	@mkdir -p bin
	@touch $@

snapshot: bin/snapshot

bin/snapshot: ${GO_SRC} $(GORELEASER)
	$(GORELEASER) check
	$(GORELEASER) release --snapshot --clean
	@mkdir -p bin
	@touch $@

release: bin/release

bin/release: ${GO_SRC} $(GORELEASER)
	$(GORELEASER) check
	$(GORELEASER) release --clean
	@mkdir -p bin
	@touch $@

chart-lint:
	docker run --rm -it -w /repo -v `pwd`:/repo quay.io/helmpack/chart-testing:v3.3.1 ct lint --all
