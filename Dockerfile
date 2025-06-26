# NOTE: The arm64 setup only works for FDB versions 7.3.
# Previous versions are missing the client packages for arm64.
ARG FDB_VERSION=7.1.67
ARG FDB_WEBSITE=https://github.com/apple/foundationdb/releases/download

# Build the manager binary
FROM docker.io/library/golang:1.24.4 AS builder

ARG FDB_VERSION
ARG FDB_WEBSITE
ARG TARGETARCH
ARG TAG="latest"

RUN set -eux && \
    if [ "$TARGETARCH" = "amd64" ]; then \
         FDB_ARCH=amd64; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
         FDB_ARCH=aarch64; \
    else \
         echo "ERROR: unsupported architecture $TARGETARCH" 1>&2; \
         exit 1; \
    fi; \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb" -o foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb.sha256" -o foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb.sha256 && \
    sha256sum -c foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb.sha256 && \
    dpkg -i foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb && \
    rm foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb foundationdb-clients_${FDB_VERSION}-1_${FDB_ARCH}.deb.sha256

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
COPY kubectl-fdb/ kubectl-fdb/

# Build
RUN CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} GO111MODULE=on make manager plugin-go

# Create user and group here since we don't have the tools
# in distroless
RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --create-home --shell /bin/bash fdb && \
	mkdir -p /var/log/fdb && \
	touch /var/log/fdb/.keep

FROM docker.io/rockylinux/rockylinux:9.5-minimal

ARG FDB_VERSION
ARG FDB_WEBSITE
ARG TARGETARCH

VOLUME /usr/lib/fdb

WORKDIR /

RUN set -eux && \
    if [ "$TARGETARCH" = "amd64" ]; then \
         FDB_ARCH=x86_64; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
         FDB_ARCH=aarch64; \
    else \
         echo "ERROR: unsupported architecture $TARGETARCH" 1>&2; \
         exit 1; \
    fi; \
    if [ "${FDB_VERSION%.*}" = "7.1" ]; then \
         # FDB 7.1 published the client packages for el7, 7.3 and newer uses el9.
         FDB_OS=el7; \
    else \
         FDB_OS=el9; \
    fi; \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm" -o foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm.sha256" -o foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm.sha256 && \
    microdnf install -y glibc pkg-config && \
    microdnf clean all && \
    sha256sum -c foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm.sha256 && \
    rpm -i foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm --excludepath=/usr/bin --excludepath=/usr/lib/foundationdb/backup_agent && \
    rm foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm foundationdb-clients-${FDB_VERSION}-1.${FDB_OS}.${FDB_ARCH}.rpm.sha256

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --chown=fdb:fdb --from=builder /workspace/bin/manager .
COPY --chown=fdb:fdb --from=builder /workspace/bin/kubectl-fdb /usr/local/bin/kubectl-fdb
COPY --chown=fdb:fdb --from=builder /var/log/fdb/.keep /var/log/fdb/.keep

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/bin/fdb
ENV FDB_NETWORK_OPTION_DISABLE_LOCAL_CLIENT=""
ENV FDB_BINARY_DIR=/usr/bin/fdb

ENTRYPOINT ["/manager"]
