#!/usr/bin/env bash

helm template --values charts/.ci/values-kube-score.yaml charts/* -f charts/.ci/values-kube-score.yaml | kube-score score - \
    --ignore-test pod-networkpolicy \
    --ignore-test deployment-has-poddisruptionbudget \
    --ignore-test deployment-has-host-podantiaffinity \
    --ignore-test container-image-pull-policy \
    --ignore-test container-security-context \
    --enable-optional-test container-security-context-privileged \
    --enable-optional-test container-security-context-readonlyrootfilesystem               
