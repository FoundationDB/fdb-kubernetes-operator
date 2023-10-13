#!/usr/bin/env bash

set -eu

# This directory provides an example of creating a cluster using the three_data_hall
# replication topology.
#
# This example is built for local testing, so it will create all of the Pods
# within a single Kubernetes cluster, but will give false locality information for the zones
# to make the processes believe they are in different locations.
#
# You can use this script to bootstrap the cluster. Once it finishes, you can
# make changes to the cluster by editing the final.yaml file and running the
# apply.bash script. You can clean up the clusters by running the delete.bash
# script.
DIR="${BASH_SOURCE%/*}"

. $DIR/functions.bash

AZ1=${AZ1:-"az1"}
AZ2=${AZ2:-"az2"}
AZ3=${AZ3:-"az3"}

applyFile "${DIR}/stage_1.yaml" "${AZ1}" '""'
checkReconciliationLoop test-cluster-${AZ1}
connectionString=$(getConnectionString test-cluster-${AZ1})

applyFile "${DIR}/final.yaml" "${AZ1}" "${connectionString}"
applyFile "${DIR}/final.yaml" ${AZ2} "${connectionString}"
applyFile "${DIR}/final.yaml" ${AZ3} "${connectionString}"

checkReconciliationLoop test-cluster-${AZ1}
checkReconciliationLoop test-cluster-${AZ2}
checkReconciliationLoop test-cluster-${AZ3}
