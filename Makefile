
# Image URL to use all building/pushing image targets
IMG ?= fdb-kubernetes-operator:latest
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

ifneq "$(FDB_WEBSITE)" ""
	docker_build_args := $(docker_build_args) --build-arg FDB_WEBSITE=$(FDB_WEBSITE)
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager samples

# Run tests
test: generate fmt vet manifests
	go test ./... -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	(kustomize build config/crd | kubectl replace -f -) || (kustomize build config/crd | kubectl create -f -)

# Uninstall CRDs from a cluster
uninstall: manifests
	kustomize build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: install manifests
	cd config/manager && kustomize edit set image controller=${IMG}
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./..."

# Build the docker image
docker-build: test
	docker build ${docker_build_args} . -t ${IMG}

# Push the docker image
docker-push:
	docker push ${IMG}

# Rebuilds, deploys, and bounces the operator
rebuild-operator: docker-build deploy bounce

bounce:
	kubectl delete pod -l app=fdb-kubernetes-operator-controller-manager

samples: config/samples/deployment.yaml

config/samples/deployment/crd.yaml: config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml
	cp config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml config/samples/deployment/crd.yaml

config/samples/deployment.yaml: manifests config/samples/deployment/crd.yaml
	kustomize build config/samples/deployment > config/samples/deployment.yaml


# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.4 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif
