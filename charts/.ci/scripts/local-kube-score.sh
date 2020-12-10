#!/bin/bash

helm template --values charts/.ci/values-kube-score.yaml charts/* -f charts/.ci/values-kube-score.yaml | kube-score score - \
    --ignore-test pod-networkpolicy \
    --ignore-test deployment-has-poddisruptionbudget \
    --ignore-test deployment-has-host-podantiaffinity