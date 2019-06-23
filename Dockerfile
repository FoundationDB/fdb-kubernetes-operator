# Build the manager binary
FROM golang:1.10.3 as builder

# Install FDB
ARG FDB_VERSION=6.1.8
ARG FDB_ADDITIONAL_VERSIONS=""
RUN mkdir -p /usr/lib/fdb/multiversion && \
	wget https://www.foundationdb.org/downloads/$FDB_VERSION/ubuntu/installers/foundationdb-clients_$FDB_VERSION-1_amd64.deb -O fdb.deb && \
	dpkg -i fdb.deb && rm fdb.deb && \
	for version in ${FDB_ADDITIONAL_VERSIONS}; do \
		wget https://www.foundationdb.org/downloads/$version/linux/libfdb_c_$version.so -O /usr/lib/fdb/multiversion/libfdb_c_${version%.*}.so; \
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
COPY --from=builder /usr/lib/fdb /usr/lib/fdb
COPY --from=builder /usr/lib/libfdb_c.so /usr/lib/libfdb_c.so
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/lib/fdb/multiversion
ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
RUN mkdir -p /var/log/fdb
ENTRYPOINT ["/manager"]
