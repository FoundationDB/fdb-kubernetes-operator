FROM docker.io/foundationdb/foundationdb:6.2.30 as fdb62
FROM docker.io/foundationdb/foundationdb:6.3.22 as fdb63

# Build the manager binary
FROM docker.io/library/golang:1.17.6 as builder

# Install FDB
ARG FDB_VERSION=6.2.30
ARG FDB_WEBSITE=https://github.com/apple/foundationdb/releases/download
ARG TAG="latest"

RUN set -eux && \
	curl --fail -L ${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_amd64.deb -o fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	mkdir -p /usr/bin/fdb/${FDB_VERSION%.*} && \
	for binary in fdbbackup fdbcli; do \
		download_path=/usr/bin/fdb/${FDB_VERSION%.*}/$binary && \
		curl --fail -L ${FDB_WEBSITE}/${FDB_VERSION}/$binary.x86_64 -o $download_path && \
		chmod u+x $download_path; \
	done && \
	mkdir -p /usr/lib/fdb

# TODO: Remove the behavior of copying binaries from the FDB images as part of the 1.0 release of the operator.

# Copy 6.2 binaries
COPY --from=fdb62 /usr/bin/fdb* /usr/bin/fdb/6.2/

# Copy 6.3 binaries
COPY --from=fdb63 /usr/bin/fdb* /usr/bin/fdb/6.3/
COPY --from=fdb63 /usr/lib/libfdb_c.so /usr/lib/fdb/libfdb_c_6.3.so

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
# https://github.com/golang/go/issues/44129#issuecomment-865249631 time to move to 1.17
RUN go env -w GOFLAGS=-mod=mod
RUN go mod download -x

# Copy the go source
COPY main.go main.go
COPY Makefile Makefile
COPY api/ api/
COPY controllers/ controllers/
COPY setup/ setup/
COPY fdbclient/ fdbclient/
COPY internal/ internal/
COPY pkg/ pkg/
COPY mock-kubernetes-client/ mock-kubernetes-client/

# Build
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on make manager

# Create user and group here since we don't have the tools
# in distroless
RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --create-home --shell /bin/bash fdb && \
	mkdir -p /var/log/fdb && \
	touch /var/log/fdb/.keep

FROM gcr.io/distroless/base

WORKDIR /

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --chown=fdb:fdb --from=builder /workspace/bin/manager .
COPY --from=builder /usr/bin/fdb /usr/bin/fdb
COPY --from=builder /usr/lib/libfdb_c.so /usr/lib/
COPY --from=builder /usr/lib/fdb /usr/lib/fdb/
COPY --chown=fdb:fdb --from=builder /var/log/fdb/.keep /var/log/fdb/.keep

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/lib/fdb

ENTRYPOINT ["/manager"]
