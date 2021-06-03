#! /bin/bash

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

applyFile $DIR/stage_1.yaml dc1 '""'
checkReconciliationLoop test-cluster-dc1
connectionString=$(getConnectionString test-cluster-dc1)

applyFile $DIR/final.yaml dc1 $connectionString
applyFile $DIR/final.yaml dc2 $connectionString
applyFile $DIR/final.yaml dc3 $connectionString

checkReconciliationLoop test-cluster-dc1
checkReconciliationLoop test-cluster-dc2
checkReconciliationLoop test-cluster-dc3
