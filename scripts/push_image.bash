#! /bin/sh

# Invocation: scripts/push_image.sh [version]
# Example: scripts/push_image.sh 1.2.0

version=$1

docker tag fdb-kubernetes-operator:latest foundationdb/fdb-kubernetes-operator:latest
docker push foundationdb/fdb-kubernetes-operator:latest
docker tag fdb-kubernetes-operator:latest foundationdb/fdb-kubernetes-operator:$version
docker push foundationdb/fdb-kubernetes-operator:$version
