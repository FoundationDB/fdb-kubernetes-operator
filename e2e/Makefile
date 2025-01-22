#!/usr/bin/make

# Default target. Want this to be first.
compile:
	go build ./...

# INPUT ENVIRONMENT VARIABLES
TIMEOUT?=168h

CLUSTER_NAME?=
NAMESPACE?=
CONTEXT?=
FDB_VERSION?=7.1.57
# This will be the version used for upgrade tests.
NEXT_FDB_VERSION?=7.3.59
## Expectation is that you are running standard build image which generates both regular and debug (Symbols) images.
FDB_IMAGE?=foundationdb/foundationdb:$(FDB_VERSION)
UNIFIED_FDB_IMAGE?=foundationdb/fdb-kubernetes-monitor:$(FDB_VERSION)
SIDECAR_IMAGE?=foundationdb/foundationdb-kubernetes-sidecar:$(FDB_VERSION)-1
OPERATOR_IMAGE?=foundationdb/fdb-kubernetes-operator:latest
REGISTRY?=docker.io
CLEANUP?=true
TEST_USERNAME?=$(USER)
ENABLE_CHAOS_TESTS?=true
CHAOS_NAMESPACE?=chaos-testing
STORAGE_CLASS?=
STORAGE_ENGINE?=
DUMP_OPERATOR_STATE?=true
SEAWEEDFS_IMAGE?=chrislusf/seaweedfs:3.73
# Defines the cloud provider used for the underlying Kubernetes cluster. Currently only kind is support, other cloud providers
# should still work but this test framework has no special cases for those.
CLOUD_PROVIDER?=
# Multiple versions can be specified for these upgrades by separating them with a, e.g. 6.2.25:7.1.25,7.1.23:7.1.25
UPGRADE_VERSIONS?="$(FDB_VERSION):$(NEXT_FDB_VERSION)"
# Those are feature flags for the operator tests. Enable a feature if you want to run the operator tests with a specific
# feature enabled e.g. like DNS.
FEATURE_DNS?=false
FEATURE_LOCALITIES?=false
# Allows to specify the tag for a specific FDB version. Format is 7.1.57:7.1.57-testing,7.3.35:7.3.35-debugging
FDB_VERSION_TAG_MAPPING?=
# If the FEATURE_SERVER_SIDE_APPLY environment variable is not defined the test suite will be randomly enable (1) or disable (0)
# the server side apply feature.
ifndef FEATURE_SERVER_SIDE_APPLY
  FEATURE_SERVER_SIDE_APPLY=$(shell shuf -i 0-1 -n 1)
endif

# If the FEATURE_UNIFIED_IMAGE environment variable is not defined the test suite will be randomly enable (1) or disable (0)
# the unified image feature.
ifndef FEATURE_UNIFIED_IMAGE
  FEATURE_UNIFIED_IMAGE=$(shell shuf -i 0-1 -n 1)
endif

# If the STORAGE_ENGINE environment variable is not defined the test suite will be randomly choosing between the ssd
# storage-engine (sqlite) or RocksDB. In the future we can add more storage engines for testing here, e.g. Redwood or
# sharded RocksDB.
ifndef STORAGE_ENGINE
  RANDOM_INT=$(shell shuf -i 0-1 -n 1)
  ifeq ($(RANDOM_INT), 0)
    # Default case
    STORAGE_ENGINE="ssd"
  else
    STORAGE_ENGINE="ssd-rocksdb-v1"
  endif
endif

# Make bash pickier about errors.
SHELL=/bin/bash -euo pipefail

# Defines a variable that has the directory of this Makefile as value.
BASE_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

clean:
	@rm -f TEST-ginkgo*.xml
	@find . -name '*~' | xargs rm -f

# This target can be used to delete namespaces that were created during testing. If a namespace is provided the user and
# the namespace (as a prefix) must match.
destroy-my-namespaces:
	./scripts/remove_namespaces ${TEST_USERNAME} ${NAMESPACE}

# This target can be used to create a kind cluster that can be used for e2e testing.
kind-setup:
	@CHAOS_NAMESPACE=$(CHAOS_NAMESPACE) FDB_VERSION=$(FDB_VERSION) UPGRADE_VERSIONS=$(UPGRADE_VERSIONS) REGISTRY=$(REGISTRY) $(shell pwd)/scripts/setup_e2e.sh

# This target will remove the kind cluster.
kind-destroy:
	@kind delete clusters e2e-tests

# This target will build the operator image and updates the image in the kind clusters.
kind-update-operator:
	@BUILD_PLATFORM="linux/amd64" make -C "$(shell pwd)/.." container-build
	@kind load docker-image "fdb-kubernetes-operator:latest" --name "e2e-tests"

# No one in the team can figure out how to get ginkgo's test launcher to properly
# manage concurrency, or control which tests are run in a predictable manner.

# This makefile reimplements that, and works around a few bugs in ginkgo itself.
ifndef QUIET
  VERBOSE=-v
  GINKGO_VERBOSE=--ginkgo.v
else
  VERBOSE=
  GINKGO_VERBOSE=
endif

# ginkgo doesn't understand unix output redirection, so we have to implement this bit for it.
ifdef MAKE_TERMOUT
	NO_COLOR=
else
	NO_COLOR=--ginkgo.no-color
endif

SUITES=$(wildcard test_*)
TARGETS=$(patsubst test_%,test_%.run,${SUITES})

run: ${TARGETS}

# This variable can be used to define any label-filter for ginkgo to run a subset of tests.
# For more information see the Ginkgo Spec labels documentation: https://onsi.github.io/ginkgo/#spec-labels.
GINKGO_LABEL_FILTER?=

# Only run tests that are labeled with the "pr" label.
pr-tests: GINKGO_LABEL_FILTER=--ginkgo.label-filter="pr"

# Run the actual pr tests.
pr-tests: run

# Only run tests that are labeled with the "pr" or the "nightly" label.
nightly-tests: GINKGO_LABEL_FILTER=--ginkgo.label-filter="pr || nightly"

# Run the actual nightly tests
nightly-tests: run

%.run: %
	@sleep $$(shuf -i 1-10 -n 1)
	go test -timeout=$(TIMEOUT) $(VERBOSE) ./$< \
	${NO_COLOR} ${GINKGO_VERBOSE} \
	  $(GINKGO_LABEL_FILTER) \
	  --ginkgo.junit-report=$@.xml \
	  --ginkgo.timeout=$(TIMEOUT) \
	  --timeout=$(TIMEOUT) \
	  --namespace="$(NAMESPACE)" \
	  --context="$(CONTEXT)" \
	  --fdb-image="$(FDB_IMAGE)" \
	  --sidecar-image="$(SIDECAR_IMAGE)" \
	  --operator-image="$(OPERATOR_IMAGE)" \
	  --registry="$(REGISTRY)" \
	  --fdb-version="$(FDB_VERSION)" \
	  --cleanup=$(CLEANUP) \
	  --username="$(TEST_USERNAME)" \
	  --chaos-namespace="$(CHAOS_NAMESPACE)" \
	  --storage-class="$(STORAGE_CLASS)" \
	  --enable-chaos-tests=$(ENABLE_CHAOS_TESTS) \
	  --upgrade-versions=$(UPGRADE_VERSIONS) \
	  --feature-unified-image=$(FEATURE_UNIFIED_IMAGE) \
	  --feature-localities=$(FEATURE_LOCALITIES) \
	  --feature-dns=$(FEATURE_DNS) \
	  --cloud-provider=$(CLOUD_PROVIDER) \
	  --dump-operator-state=$(DUMP_OPERATOR_STATE) \
	  --cluster-name=$(CLUSTER_NAME) \
	  --storage-engine=$(STORAGE_ENGINE) \
	  --fdb-version-tag-mapping=$(FDB_VERSION_TAG_MAPPING) \
	  --unified-fdb-image=$(UNIFIED_FDB_IMAGE) \
	  --feature-server-side-apply=$(FEATURE_SERVER_SIDE_APPLY) \
	  --seaweedfs-image=$(SEAWEEDFS_IMAGE) \
	  | grep -v 'constructing many client instances from the same exec auth config can cause performance problems during cert rotation' &> $(BASE_DIR)/../logs/$<.log
