# Build the manager binary
FROM golang:1.13 as builder

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
	done && \
	mkdir -p /var/log/fdb

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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

FROM ubuntu:latest
WORKDIR /

COPY --from=builder /workspace/manager .
COPY --from=builder /usr/bin/fdb /usr/bin/fdb
COPY --from=builder /var/log/fdb /var/log/fdb
ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb

ENTRYPOINT ["/manager"]
