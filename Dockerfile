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

FROM docker.io/rockylinux:9.2-minimal

ARG FDB_VERSION
ARG FDB_WEBSITE

VOLUME /usr/lib/fdb

WORKDIR /

# Add tini to the operator image to allow to take coredumps.
ENV TINI_VERSION v0.19.0
RUN curl --fail -L "https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini" -o /tini && \
    curl --fail -L "https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini.asc" -o /tini.asc && \
    gpg --batch --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 595E85A6B1B4779EA4DAAEC70B588DFF0527A9B7 && \
    gpg --batch --verify /tini.asc /tini
RUN chmod +x /tini

RUN set -eux && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm" -o foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm && \
    curl --fail -L "${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256" -o foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256 && \
    microdnf install -y glibc && \
    microdnf clean all && \
    # TODO(johscheuer): The 6.2.29 sha256 file is not well formatted, enable this check again once 7.1 is used as base. \
    # sha256sum -c foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256 && \
	rpm -i foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm --excludepath=/usr/bin --excludepath=/usr/lib/foundationdb/backup_agent && \
    rm foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm foundationdb-clients-${FDB_VERSION}-1.el7.x86_64.rpm.sha256

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --chown=fdb:fdb --from=builder /workspace/bin/manager .
COPY --chown=fdb:fdb --from=builder /var/log/fdb/.keep /var/log/fdb/.keep

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENV FDB_NETWORK_OPTION_TRACE_LOG_GROUP=fdb-kubernetes-operator
ENV FDB_NETWORK_OPTION_TRACE_ENABLE=/var/log/fdb
ENV FDB_BINARY_DIR=/usr/bin/fdb
ENV FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY=/usr/bin/fdb

ENTRYPOINT ["/tini", "--", "/manager"]
