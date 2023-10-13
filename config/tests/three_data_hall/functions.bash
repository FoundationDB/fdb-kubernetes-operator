function applyFile() {
	az=${2}

	az="${az}" connectionString="${3}" envsubst < "${1}"| kubectl apply -f -
}

function checkReconciliation() {
	clusterName=$1

	generationsOutput=$(kubectl get fdb "${clusterName}" -o jsonpath='{.metadata.generation} {.status.generations.reconciled}')
	read -ra generations <<< "${generationsOutput}"
	if [[ ("${#generations[@]}" -ge 2) && ("${generations[0]}" == "${generations[1]}") ]]; then
		return 1
	else
		echo "Latest generations for $clusterName: $generationsOutput"
		return 0
	fi
}

function getConnectionString() {
	kubectl get fdb "${1}" -o jsonpath='{.status.connectionString}'
}

function checkReconciliationLoop() {
	while checkReconciliation "${1}" ; do
		echo "Waiting for reconciliation"
		sleep 5
	done
}
