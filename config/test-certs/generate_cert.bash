#! /bin/bash

# generate_cert.bash
#
# This source file is part of the FoundationDB open source project
#
# Copyright 2018-2019 Apple Inc. and the FoundationDB project authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# This script generates a self-signed cert for local testing.
# The cert will be stored in the repository, so you do not need to run this
# yourself unless the certificate has expired.

openssl req -x509 -newkey rsa:4096 -nodes -keyout config/test-certs/key.pem -out config/test-certs/cert.pem -days 365 -subj '/CN=localhost' -reqexts SAN -extensions SAN -config <(cat /etc/ssl/openssl.cnf <(printf "[SAN]\nsubjectAltName=DNS:minio-service")) 