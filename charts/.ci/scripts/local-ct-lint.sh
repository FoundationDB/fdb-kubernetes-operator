#!/bin/bash

<<<<<<< HEAD
docker run --rm -it -w /repo -v $(pwd):/repo quay.io/helmpack/chart-testing ct lint --all
=======
docker run --rm -it -w /repo -v `pwd`:/repo quay.io/helmpack/chart-testing ct lint --all
>>>>>>> 3b9f0ff (Update chart and add publishing action)
