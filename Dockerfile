# Build the manager binary
FROM golang:1.15.5 as builder

# Install FDB
ARG FDB_VERSION=6.2.27
ARG FDB_WEBSITE=https://www.foundationdb.org

COPY foundationdb-kubernetes-sidecar/website/ /mnt/website/

# FIXME: Workaround for (https://github.com/FoundationDB/fdb-kubernetes-operator/issues/252#issuecomment-643812649)
# adds GeoTrust_Global_CA.crt during install and removes it afterwards
COPY ./foundationdb-kubernetes-sidecar/files/GeoTrust_Global_CA.pem /usr/local/share/ca-certificates/GeoTrust_Global_CA.crt
RUN set -eux && \
	update-ca-certificates --fresh && \
	curl --fail $FDB_WEBSITE/downloads/$FDB_VERSION/ubuntu/installers/foundationdb-clients_$FDB_VERSION-1_amd64.deb -o fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	mkdir -p /usr/lib/fdb && \
	rm /usr/local/share/ca-certificates/GeoTrust_Global_CA.crt && \
	update-ca-certificates --fresh

# Copy 6.2 binaries
COPY --from=foundationdb/foundationdb:6.2.27 /usr/bin/fdb* /usr/bin/fdb/6.2/

# Copy 6.1 binaries
COPY --from=foundationdb/foundationdb:6.1.13 /usr/bin/fdb* /usr/bin/fdb/6.1/
COPY --from=foundationdb/foundationdb:6.1.13 /usr/lib/libfdb_c.so /usr/lib/fdb/libfdb_c_6.1.so

# Copy 6.3 binaries
COPY --from=foundationdb/foundationdb:6.3.5 /usr/bin/fdb* /usr/bin/fdb/6.3/
COPY --from=foundationdb/foundationdb:6.3.5 /usr/lib/libfdb_c.so /usr/lib/fdb/libfdb_c_6.3.so

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

# Build
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

# Create user and group here since we don't have the tools
# in distroless
RUN groupadd --gid 40590 fdb && \
	useradd --gid 40590 --uid 40590 --create-home --shell /bin/bash fdb && \
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

USER 40590

ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/lib/fdb

ENTRYPOINT ["/manager"]
