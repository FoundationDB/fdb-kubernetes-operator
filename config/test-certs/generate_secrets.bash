#! /bin/bash

kubectl delete secrets -l app=fdb-kubernetes-operator
kubectl create secret generic fdb-kubernetes-operator-secrets --from-file=config/test-certs/key.pem --from-file=config/test-certs/cert.pem
kubectl patch secret fdb-kubernetes-operator-secrets --type='json' -p='[{"op": "add", "path": "/metadata/labels", "value":{"app":"fdb-kubernetes-operator"}}]'