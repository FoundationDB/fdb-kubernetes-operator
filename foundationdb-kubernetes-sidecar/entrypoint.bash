#! /bin/bash

export FDB_PUBLIC_IP=$(grep `hostname` /etc/hosts | sed -e "s/\s *`hostname`.*//")
if [[ $COPY_ONCE -eq 1 ]]; then
  python sidecar.py
else
  gunicorn -w 4 -b 0.0.0.0:$LISTEN_PORT sidecar:app
fi