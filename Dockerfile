FROM foundationdb/foundationdb:6.2.30 as fdb62
FROM foundationdb/foundationdb:6.1.13 as fdb61
FROM foundationdb/foundationdb:6.3.10 as fdb63

# Build the manager binary
FROM golang:1.15.8 as builder

# Install FDB
ARG FDB_VERSION=6.2.30
ARG FDB_WEBSITE=https://www.foundationdb.org

COPY foundationdb-kubernetes-sidecar/website/ /mnt/website/

RUN set -eux && \
	curl --fail ${FDB_WEBSITE}/downloads/${FDB_VERSION}/ubuntu/installers/foundationdb-clients_${FDB_VERSION}-1_amd64.deb -o fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	curl --fail $FDB_WEBSITE/downloads/$FDB_VERSION/linux/fdb_$FDB_VERSION.tar.gz -o fdb_$FDB_VERSION.tar.gz && \
	tar -xzf fdb_$FDB_VERSION.tar.gz --strip-components=1 && \
	rm fdb_$FDB_VERSION.tar.gz && \
	chmod u+x fdbbackup fdbcli fdbdr fdbmonitor fdbrestore fdbserver backup_agent dr_agent && \
	mkdir -p /usr/bin/fdb/${FDB_VERSION%.*} && \
	mv fdbbackup fdbcli fdbdr fdbmonitor fdbrestore fdbserver backup_agent dr_agent /usr/bin/fdb/${FDB_VERSION%.*} && \
	mkdir -p /usr/lib/fdb

# TODO: Remove the behavior of copying binaries from the FDB images as part of the 1.0 release of the operator.

# Copy 6.2 binaries
COPY --from=fdb62 /usr/bin/fdb* /usr/bin/fdb/6.2/

# Copy 6.1 binaries
COPY --from=fdb61 /usr/bin/fdb* /usr/bin/fdb/6.1/
COPY --from=fdb61 /usr/lib/libfdb_c.so /usr/lib/fdb/libfdb_c_6.1.so

# Copy 6.3 binaries
COPY --from=fdb63 /usr/bin/fdb* /usr/bin/fdb/6.3/
COPY --from=fdb63 /usr/lib/libfdb_c.so /usr/lib/fdb/libfdb_c_6.3.so

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY setup/ setup/
COPY fdbclient/ fdbclient/

# Build
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

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
COPY --chown=fdb:fdb --from=builder /workspace/manager .
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
