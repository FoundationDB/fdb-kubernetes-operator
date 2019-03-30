
# Image URL to use all building/pushing image targets
IMG ?= fdb-kubernetes-operator:latest

all: test manager

PKG_PATH = github.com/foundationdb/fdb-kubernetes-operator

ifneq "$(DOCKER_IMAGE_ROOT)" ""
	go_subs := $(go_subs) -X ${PKG_PATH}/pkg/controller/foundationdbcluster.DockerImageRoot=$(DOCKER_IMAGE_ROOT)
endif

ifneq "$(go_subs)" ""
	go_ld_flags := -ldflags "$(go_subs)"
endif


# Run tests
test: generate fmt vet manifests
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build $(go_ld_flags) -o bin/manager ${PKG_PATH}/cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run $(go_ld_flags) ./cmd/manager/main.go

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crds

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests templates
	kubectl apply -f config/crds
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	go run vendor/sigs.k8s.io/controller-tools/cmd/controller-gen/main.go all

templates: config/default/manager_image_patch.yaml

config/default/manager_image_patch.yaml:
	cp config/default/manager_image_patch.yaml.sample config/default/manager_image_patch.yaml


# Run go fmt against code
fmt:
	go fmt ./pkg/... ./cmd/...

# Run go vet against code
vet:
	go vet ./pkg/... ./cmd/...

# Generate code
generate:
ifndef GOPATH
	$(error GOPATH not defined, please define GOPATH. Run "go help gopath" to learn more about GOPATH)
endif
	go generate ./pkg/... ./cmd/...

# Build the docker image
docker-build: test templates
	docker build --build-arg "GO_BUILD_SUBS=${go_subs}" . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
docker-push: docker-build
	docker push ${IMG}

# Rebuilds, deploys, and bounces the operator
rebuild-operator: docker-build deploy
	kubectl delete pod fdb-kubernetes-operator-controller-manager-0
