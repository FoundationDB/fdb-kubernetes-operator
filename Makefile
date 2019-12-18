
# Image URL to use all building/pushing image targets
IMG ?= fdb-kubernetes-operator:latest

all: test manager samples

PKG_PATH = github.com/foundationdb/fdb-kubernetes-operator

ifneq "$(FDB_WEBSITE)" ""
	docker_build_args := $(docker_build_args) --build-arg FDB_WEBSITE=$(FDB_WEBSITE)
endif

# Run tests
test: generate fmt vet manifests
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager ${PKG_PATH}/cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run ./cmd/manager/main.go

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

samples: config/samples/deployment.yaml

config/samples/deployment/crd.yaml: config/crds/apps_v1beta1_foundationdbcluster.yaml
	cp config/crds/apps_v1beta1_foundationdbcluster.yaml config/samples/deployment/crd.yaml
config/samples/deployment.yaml: manifests templates config/samples/deployment/crd.yaml
	kustomize build config/samples/deployment > config/samples/deployment.yaml

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
	docker build ${docker_build_args} . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
docker-push:
	docker push ${IMG}

# Rebuilds, deploys, and bounces the operator
rebuild-operator: docker-build deploy
	kubectl delete pod fdb-kubernetes-operator-controller-manager-0
