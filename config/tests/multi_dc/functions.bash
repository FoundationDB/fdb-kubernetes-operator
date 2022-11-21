function applyFile() {
	dc=${2}
	kubeconfig=${4}

	if [[ "${dc}" == "dc3" ]]; then
		logCount=0
	else
		logCount=-1
	fi

	dc="${dc}" connectionString="${3}" logCount="${logCount}" envsubst < "${1}"| kubectl --kubeconfig "${kubeconfig}" apply -f -
}

function checkReconciliation() {
	clusterName=$1
	kubeconfig=$2

	generationsOutput=$(kubectl --kubeconfig "${kubeconfig}" get fdb "${clusterName}" -o jsonpath='{.metadata.generation} {.status.generations.reconciled}')
	read -ra generations <<< "${generationsOutput}"
	if [[ ("${generations[1]}" != "")  && ("${generations[0]}" == "${generations[1]}") ]]; then
		return 1
	else
		echo "Latest generations for $clusterName: $generationsOutput"
		return 0
	fi
}

function getConnectionString() {
	clusterName=$1
	kubeconfig=$2

	kubectl --kubeconfig "${kubeconfig}" get fdb "${clusterName}" -o jsonpath='{.status.connectionString}'
}

function checkReconciliationLoop() {
	reconciled=0
	name=$1
	kubeconfig=$2

	while [ $reconciled -ne 1 ] ; do
		checkReconciliation "${name}" "${kubeconfig}"
		reconciled=$?
		if [[ $reconciled -ne 1 ]]; then
			echo "Waiting for reconciliation"
			sleep 5
		fi
	done
}
