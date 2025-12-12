ARG FDB_VERSION=7.1.67
ARG FDB_WEBSITE=https://github.com/apple/foundationdb/releases/download

# Build the manager binary
FROM golang:1.24.9-bookworm AS builder

ARG FDB_VERSION
ARG FDB_WEBSITE
ARG TARGETARCH
ARG TAG="latest"

RUN set -eux && \
    if [ "$TARGETARCH" = "amd64" ]; then \
         FDB_ARCH=amd64; \
    elif [ "$TARGETARCH" = "arm64" ]; then \
         FDB_ARCH=aarch64; \
         if [ "${FDB_VERSION%.*}" = "7.1" ]; then \
            FDB_VERSION="7.3.71"; \
         fi; \
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

# Build
RUN CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} GO111MODULE=on go build -o /workspace/bin/data-loader main.go

FROM rockylinux/rockylinux:9.6-minimal

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
         if [ "${FDB_VERSION%.*}" = "7.1" ]; then \
            FDB_VERSION="7.3.71"; \
         fi; \
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

RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --shell /usr/sbin/nologin fdb && \
	mkdir -p /var/log/fdb && \
	touch /var/log/fdb/.keep

COPY --chown=fdb:fdb --from=builder /workspace/bin/data-loader /usr/local/bin/data-loader

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENTRYPOINT ["/usr/local/bin/data-loader"]
