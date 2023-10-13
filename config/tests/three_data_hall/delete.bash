#!/usr/bin/env bash

set -eu

kubectl delete fdb -l cluster-group=test-cluster
