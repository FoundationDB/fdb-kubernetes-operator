# Build the manager binary
FROM golang:1.12.5 as builder

# Install FDB
ARG FDB_VERSION=6.1.8
ARG FDB_ADDITIONAL_VERSIONS="6.0.18"
RUN \
	wget https://www.foundationdb.org/downloads/$FDB_VERSION/ubuntu/installers/foundationdb-clients_$FDB_VERSION-1_amd64.deb -O fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	for version in ${FDB_VERSION} ${FDB_ADDITIONAL_VERSIONS}; do \
		minor=${version%.*} && \
		mkdir -p /usr/bin/fdb/$minor && \
		wget https://www.foundationdb.org/downloads/$version/linux/fdb_$version.tar.gz -O /usr/bin/fdb/$minor/binaries.tar.gz && \
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
