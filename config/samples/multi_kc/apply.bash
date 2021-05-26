#! /bin/bash

DIR="${BASH_SOURCE%/*}"

. $DIR/functions.bash

originalConnectionString=$(getConnectionString sample-cluster-dc1)

function applyAll() {
	applyFile $DIR/final.yaml dc1 $1
	applyFile $DIR/final.yaml dc2 $1
	applyFile $DIR/final.yaml dc3 $1

	checkReconciliationLoop sample-cluster-dc1
	checkReconciliationLoop sample-cluster-dc2
	checkReconciliationLoop sample-cluster-dc3
}

applyAll $originalConnectionString

for dc in dc1 dc2 dc3; do
	connectionString=$(getConnectionString sample-cluster-$dc)
	if [[ $connectionString != $originalConnectionString ]]; then
		applyAll $connectionString
		originalConnectionString=$connectionString
	fi
done
