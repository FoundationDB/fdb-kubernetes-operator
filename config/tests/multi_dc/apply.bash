#! /bin/bash

DIR="${BASH_SOURCE%/*}"

. $DIR/functions.bash


originalConnectionString=$(getConnectionString test-cluster-dc1)

function applyAll() {
	applyFile $DIR/final.yaml dc1 $1
	applyFile $DIR/final.yaml dc2 $1
	applyFile $DIR/final.yaml dc3 $1

	checkReconciliationLoop test-cluster-dc1
	checkReconciliationLoop test-cluster-dc2
	checkReconciliationLoop test-cluster-dc3
}

applyAll $originalConnectionString

for dc in dc1 dc2 dc3; do
	connectionString=$(getConnectionString test-cluster-$dc)
	if [[ $connectionString != $originalConnectionString ]]; then
		applyAll $connectionString
		originalConnectionString=$connectionString
	fi
done
