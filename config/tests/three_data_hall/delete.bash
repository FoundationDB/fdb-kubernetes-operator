#!/usr/bin/env bash

set -eu

kubectl -n "${NAMESPACE}" delete fdb -l cluster-group=test-cluster
