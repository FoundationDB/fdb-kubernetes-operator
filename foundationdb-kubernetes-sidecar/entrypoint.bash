#! /bin/bash

if [[ -n "$ADDITIONAL_ENV_FILE" ]]; then
  source $ADDITIONAL_ENV_FILE
fi

python sidecar.py $*