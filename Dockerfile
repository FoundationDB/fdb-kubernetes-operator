# Dockerfile
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

# Build the manager binary
FROM golang:1.12.5 as builder

# Install FDB
ARG FDB_VERSION=6.2.11
ARG FDB_ADDITIONAL_VERSIONS="6.1.8 6.0.18"
ARG FDB_WEBSITE=https://www.foundationdb.org

COPY foundationdb-kubernetes-sidecar/website/ /mnt/website/

RUN \
	curl $FDB_WEBSITE/downloads/$FDB_VERSION/ubuntu/installers/foundationdb-clients_$FDB_VERSION-1_amd64.deb -o fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	for version in ${FDB_VERSION} ${FDB_ADDITIONAL_VERSIONS}; do \
		minor=${version%.*} && \
		mkdir -p /usr/bin/fdb/$minor && \
		curl $FDB_WEBSITE/downloads/$version/linux/fdb_$version.tar.gz -o /usr/bin/fdb/$minor/binaries.tar.gz && \
		tar --strip-components=1 -C /usr/bin/fdb/$minor -xzf /usr/bin/fdb/$minor/binaries.tar.gz && \
		rm /usr/bin/fdb/$minor/binaries.tar.gz && \
		for binary in fdbserver fdbmonitor backup_agent dr_agent; do \
			rm /usr/bin/fdb/$minor/$binary; \
		done && \
		chmod u+x /usr/bin/fdb/$minor/fdb*; \
	done

# Copy in the go src
WORKDIR /go/src/github.com/foundationdb/fdb-kubernetes-operator
COPY pkg/    pkg/
COPY cmd/    cmd/
COPY vendor/ vendor/

# Build
ARG GO_BUILD_SUBS=""
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "$GO_BUILD_SUBS" -a -o manager github.com/foundationdb/fdb-kubernetes-operator/cmd/manager


# Copy the controller-manager into a thin image
FROM ubuntu:latest
WORKDIR /
COPY --from=builder /go/src/github.com/foundationdb/fdb-kubernetes-operator/manager .
COPY --from=builder /usr/bin/fdb /usr/bin/fdb
ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb
RUN mkdir -p /var/log/fdb
ENTRYPOINT ["/manager"]
