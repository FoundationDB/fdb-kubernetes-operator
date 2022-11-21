function applyFile() {
	dc=${2}

	if [[ "${dc}" == "dc3" ]]; then
		logCount=0
	else
		logCount=-1
	fi

	dc="${dc}" connectionString="${3}" logCount="${logCount}" envsubst < "${1}"| kubectl --kubeconfig "${4}" apply -f -
}

function checkReconciliation() {
	clusterName=$1

	generationsOutput=$(kubectl --kubeconfig "${2}" get fdb "${clusterName}" -o jsonpath='{.metadata.generation} {.status.generations.reconciled}')
	read -ra generations <<< "${generationsOutput}"
	if [[ ("${#generations[@]}" -ge 2) && ("${generations[0]}" == "${generations[1]}") ]]; then
		return 1
	else
		echo "Latest generations for $clusterName: $generationsOutput"
		return 0
	fi
}

function getConnectionString() {
	kubectl --kubeconfig "${2}" get fdb "${1}" -o jsonpath='{.status.connectionString}'
}

function checkReconciliationLoop() {
	while checkReconciliation "${1}" "${2}"; do
		echo "Waiting for reconciliation"
		sleep 5
	done
}
