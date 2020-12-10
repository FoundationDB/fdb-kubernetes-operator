#!/bin/bash

helm template --values charts/.ci/values-kube-score.yaml charts/* -f charts/.ci/values-kube-score.yaml | kube-score score - \
    --ignore-test pod-networkpolicy \
    --ignore-test deployment-has-poddisruptionbudget \
<<<<<<< HEAD
    --ignore-test deployment-has-host-podantiaffinity \
    --enable-optional-test container-security-context-privileged \
    --enable-optional-test container-security-context-readonlyrootfilesystem \
    --ignore-test container-security-context    

=======
    --ignore-test deployment-has-host-podantiaffinity
>>>>>>> 3b9f0ff (Update chart and add publishing action)
