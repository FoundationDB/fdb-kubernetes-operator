.DEFAULT_GOAL=all

# Image URL to use all building/pushing image targets
IMG ?= fdb-kubernetes-operator:latest
CRD_OPTIONS ?= "crd:trivialVersions=true,maxDescLen=0,crdVersions=v1,generateEmbeddedObjectMeta=true"

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
CONTROLLER_GEN_PKG?=sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.1
CONTROLLER_GEN=$(GOBIN)/controller-gen
KUSTOMIZE_PKG?=sigs.k8s.io/kustomize/kustomize/v3@v3.9.4
KUSTOMIZE=$(GOBIN)/kustomize
YQ_PKG?=github.com/mikefarah/yq/v4@v4.13.5
YQ=$(GOBIN)/yq
GOLANGCI_LINT_PKG=github.com/golangci/golangci-lint/cmd/golangci-lint@v1.42.1
GOLANGCI_LINT=$(GOBIN)/golangci-lint
BUILD_DEPS?=
BUILDER?="docker"

define godep
BUILD_DEPS+=$(1)
$($2):
	(export TMP_DIR=$$$$(mktemp -d) ;\
	cd $$$$TMP_DIR ;\
	go get $$($2_PKG) ;\
	cd - ;\
	rm -rf $$$$TMP_DIR )
$(1): $$($2)
endef

$(eval $(call godep,controller-gen,CONTROLLER_GEN))
$(eval $(call godep,golangci-lint,GOLANGCI_LINT))
$(eval $(call godep,kustomize,KUSTOMIZE))
$(eval $(call godep,yq,YQ))

GO_SRC=$(shell find . -name "*.go" -not -name "zz_generated.*.go")
GENERATED_GO=api/v1beta1/zz_generated.deepcopy.go
GO_ALL=${GO_SRC} ${GENERATED_GO}
MANIFESTS=config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml

ifeq "$(TEST_RACE_CONDITIONS)" "1"
	go_test_flags := $(go_test_flags) -race -timeout=30m
endif


all: deps generate fmt vet manager plugin manifests samples documentation test_if_changed

.PHONY: clean all manager samples documentation run install uninstall deploy manifests fmt vet generate container-build container-push rebuild-operator bounce lint

deps: $(BUILD_DEPS)

clean:
	find config/crd/bases -type f -name "*.yaml" -delete
	find api -type f -name "zz_generated.*.go" -delete
	mkdir -p bin
	rm -r bin
	find config/samples -type f -name deployment.yaml -delete
	find . -name "cover.out" -delete

clean-deps:
	@rm $(CONTROLLER_GEN) $(KUSTOMIZE) $(KUBEBUILDER) $(GOLANGCI_LINT)

# Run tests
test:
ifneq "$(SKIP_TEST)" "1"
	go test ${go_test_flags} ./... -coverprofile cover.out
endif

test_if_changed: cover.out

cover.out: ${GO_ALL} ${MANIFESTS}
ifneq "$(SKIP_TEST)" "1"
	go test ${go_test_flags} ./... -coverprofile cover.out -tags test
endif

# Build manager binary
manager: bin/manager

bin/manager: ${GO_SRC}
	go build -ldflags="-s -w -X github.com/FoundationDB/fdb-kubernetes-operator/setup.operatorVersion=${TAG}" -o bin/manager main.go

# package the plugin
package: plugin bin/kubectl-fdb.tar.gz

bin/kubectl-fdb.tar.gz:
	cp ./kubectl-fdb/Readme.md ./bin
	(cd ./bin; tar cfvz ./kubectl-fdb.tar.gz ./kubectl-fdb ./Readme.md)

# Build kubectl-fdb binary
plugin: bin/kubectl-fdb

bin/kubectl-fdb: ${GO_SRC}
	go build -ldflags="-s -w -X github.com/FoundationDB/fdb-kubernetes-operator/kubectl-fdb/cmd.pluginVersion=${TAG}" -o bin/kubectl-fdb ./kubectl-fdb

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate manifests
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	kustomize build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	kustomize build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: install manifests
	cd config/manager && kustomize edit set image controller=${IMG}
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: ${MANIFESTS}

${MANIFESTS}: ${CONTROLLER_GEN} ${GO_SRC}
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	# See: https://github.com/kubernetes-sigs/controller-tools/issues/476 remove after the next release (and add a note in the release)
	yq e '.spec.preserveUnknownFields = false' -i ./config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml
	yq e '.spec.preserveUnknownFields = false' -i ./config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml
	yq e '.spec.preserveUnknownFields = false' -i ./config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml

# Run go fmt against code
fmt: bin/fmt_check

bin/fmt_check: ${GO_ALL}
	gofmt -w -s .
	mkdir -p bin
	@touch bin/fmt_check

# Run go vet against code
vet: bin/vet_check

bin/vet_check: ${GO_ALL}
	go vet ./...
	mkdir -p bin
	@touch bin/vet_check

# Generate code
generate: ${GENERATED_GO}

${GENERATED_GO}: ${GO_SRC} hack/boilerplate.go.txt ${CONTROLLER_GEN}
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./..."

# Build the container image
container-build: test_if_changed
	$(BUILDER) build --build-arg=TAG=${TAG} ${img_build_args} -t ${IMG} .

# Push the container image
container-push:
	$(BUILDER) push ${IMG}

# Rebuilds, deploys, and bounces the operator
rebuild-operator: container-build deploy bounce

bounce:
	kubectl delete pod -l app=fdb-kubernetes-operator-controller-manager

samples: config/samples/deployment.yaml

config/samples/deployment.yaml: config/samples/deployment/*.yaml
	kustomize build config/samples/deployment > config/samples/deployment.yaml

bin/po-docgen: cmd/po-docgen/*.go
	go build -o bin/po-docgen cmd/po-docgen/main.go  cmd/po-docgen/api.go

docs/cluster_spec.md: bin/po-docgen api/v1beta1/foundationdbcluster_types.go
	bin/po-docgen api api/v1beta1/foundationdbcluster_types.go > docs/cluster_spec.md

docs/backup_spec.md: bin/po-docgen api/v1beta1/foundationdbbackup_types.go
	bin/po-docgen api api/v1beta1/foundationdbbackup_types.go > docs/backup_spec.md

docs/restore_spec.md: bin/po-docgen api/v1beta1/foundationdbrestore_types.go
	bin/po-docgen api api/v1beta1/foundationdbrestore_types.go > docs/restore_spec.md

documentation: docs/cluster_spec.md docs/backup_spec.md docs/restore_spec.md

lint:
	golangci-lint run ./...

