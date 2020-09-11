# Build the manager binary
FROM golang:1.13.15 as builder

# Install FDB
ARG FDB_VERSION=6.2.22
ARG FDB_ADDITIONAL_VERSIONS="6.1.13"
ARG FDB_WEBSITE=https://www.foundationdb.org

COPY foundationdb-kubernetes-sidecar/website/ /mnt/website/

# FIXME: Workaround for (https://github.com/FoundationDB/fdb-kubernetes-operator/issues/252#issuecomment-643812649)
# adds GeoTrust_Global_CA.crt during install and removes it afterwards
COPY ./foundationdb-kubernetes-sidecar/files/GeoTrust_Global_CA.pem /usr/local/share/ca-certificates/GeoTrust_Global_CA.crt
RUN set -eux && \
    update-ca-certificates --fresh && \
	curl $FDB_WEBSITE/downloads/$FDB_VERSION/ubuntu/installers/foundationdb-clients_$FDB_VERSION-1_amd64.deb -o fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	for version in ${FDB_VERSION} ${FDB_ADDITIONAL_VERSIONS}; do \
		minor=${version%.*} && \
		mkdir -p /usr/bin/fdb/$minor/tmp && \
		curl $FDB_WEBSITE/downloads/$version/linux/fdb_$version.tar.gz -o /usr/bin/fdb/$minor/binaries.tar.gz && \
		tar --strip-components=1 -C /usr/bin/fdb/$minor/tmp -xzf /usr/bin/fdb/$minor/binaries.tar.gz && \
		rm /usr/bin/fdb/$minor/binaries.tar.gz && \
		for binary in fdbcli fdbbackup fdbrestore; do \
			mv /usr/bin/fdb/$minor/tmp/$binary /usr/bin/fdb/$minor/$binary; \
		done && \
		rm -r /usr/bin/fdb/$minor/tmp && \
		chmod u+x /usr/bin/fdb/$minor/fdb*; \
	done && \
	mkdir -p /usr/lib/fdb && \
	for VERSION in ${FDB_ADDITIONAL_VERSIONS}; do \
		curl $FDB_WEBSITE/downloads/$VERSION/linux/libfdb_c_$VERSION.so -o /usr/lib/fdb/libfdb_c_$VERSION.so; \
	done && \
    rm /usr/local/share/ca-certificates/GeoTrust_Global_CA.crt && \
    update-ca-certificates --fresh

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

FROM ubuntu:18.04
WORKDIR /

COPY --from=builder /workspace/manager .
COPY --from=builder /usr/bin/fdb /usr/bin/fdb
COPY --from=builder /usr/lib/libfdb_c.so /usr/lib/
COPY --from=builder /usr/lib/fdb /usr/lib/fdb/

RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --create-home --shell /bin/bash fdb && \
	mkdir -p /var/log/fdb && \
	chown fdb:fdb /var/log/fdb && \
	chmod -R a+x /usr/bin/fdb

USER fdb
ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/lib/fdb

ENTRYPOINT ["/manager"]
