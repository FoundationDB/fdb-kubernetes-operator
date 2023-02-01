#!/usr/bin/env bash

set -eu

# This directory provides an example of creating a cluster using the multi-KC
# replication topology.
#
# This example is built for local testing, so it will create all of the pods
# within a single KC, but will give false locality information for the zones
# to make the processes believe they are in different locations.
#
# You can use this script to bootstrap the cluster. Once it finishes, you can
# make changes to the cluster by editing the final.yaml file and running the
# apply.bash script. You can clean up the clusters by running the delete.bash
# script.
DIR="${BASH_SOURCE%/*}"

. $DIR/functions.bash

# To test a multi-region FDB cluster setup we need to have 3 Kubernetes clusters
cluster1=${CLUSTER1:-cluster1}
cluster2=${CLUSTER2:-cluster2}
cluster3=${CLUSTER3:-cluster3}

# For all the clusters we have to create tha according kubeconfig
kubeconfig1=${KUBECONFIG1:-"${cluster1}.kubeconfig"}
kubeconfig2=${KUBECONFIG2:-"${cluster2}.kubeconfig"}
kubeconfig3=${KUBECONFIG3:-"${cluster3}.kubeconfig"}

applyFile $DIR/stage_1.yaml dc1 '""' "${kubeconfig1}"
checkReconciliationLoop test-cluster-dc1 "${kubeconfig1}"
connectionString=$(getConnectionString test-cluster-dc1 "${kubeconfig1}")

applyFile $DIR/final.yaml dc1 $connectionString "${kubeconfig1}"
applyFile $DIR/final.yaml dc2 $connectionString "${kubeconfig2}"
applyFile $DIR/final.yaml dc3 $connectionString "${kubeconfig3}"

checkReconciliationLoop test-cluster-dc1 "${kubeconfig1}"
checkReconciliationLoop test-cluster-dc2 "${kubeconfig2}"
checkReconciliationLoop test-cluster-dc3 "${kubeconfig3}"
