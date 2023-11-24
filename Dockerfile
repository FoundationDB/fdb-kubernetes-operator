ARG BASE_IMAGE=docker.io/rockylinux:9.2-minimal
ARG FDB_VERSION=6.2.29
ARG FDB_WEBSITE=https://github.com/apple/foundationdb/releases/download

# Build the manager binary
FROM docker.io/library/golang:1.20.11 as builder

ARG FDB_VERSION
ARG FDB_WEBSITE
ARG TAG="latest"

RUN set -eux && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_amd64.deb" -o foundationdb-clients_${FDB_VERSION}-1_amd64.deb && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_amd64.deb" -o foundationdb-clients_${FDB_VERSION}-1_amd64.deb.sha256 && \
    # TODO(johscheuer): The 6.2.29 sha256 file is not well formatted, enable this check again once 7.1 is used as base. \
    # sha256sum -c foundationdb-clients_${FDB_VERSION}-1_amd64.deb.sha256 && \
	dpkg -i foundationdb-clients_${FDB_VERSION}-1_amd64.deb && \
    rm foundationdb-clients_${FDB_VERSION}-1_amd64.deb foundationdb-clients_${FDB_VERSION}-1_amd64.deb.sha256

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
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

FROM $BASE_IMAGE

ARG FDB_VERSION
ARG FDB_WEBSITE

VOLUME /usr/lib/fdb

WORKDIR /

RUN set -eux && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm" -o foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256" -o foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256 && \
    microdnf install -y glibc && \
    # TODO(johscheuer): The 6.2.29 sha256 file is not well formatted, enable this check again once 7.1 is used as base. \
    # sha256sum -c foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256 && \
	rpm -i foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm && \
    rm foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --chown=fdb:fdb --from=builder /workspace/bin/manager .
COPY --chown=fdb:fdb --from=builder /var/log/fdb/.keep /var/log/fdb/.keep

# FoundationDB versions newer than 7.1.33 are complied with the AWS SDK per default and therefore require
# some additional libraries that are not present in the default bookwork image. We copy those
# libraries to make the distroless version of the operator work too, otherwise we could just install
# the required packages.
COPY --from=builder /usr/lib/x86_64-linux-gnu/lib* /usr/lib/x86_64-linux-gnu/

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/bin/fdb

ENTRYPOINT ["/manager"]
