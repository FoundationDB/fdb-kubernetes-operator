#!/usr/bin/env bash

docker run --rm -it -w /repo -v `pwd`:/repo quay.io/helmpack/chart-testing:v3.3.1 ct lint --all
