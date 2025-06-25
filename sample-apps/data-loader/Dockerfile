FROM docker.io/library/python:3.13-slim

ARG TARGETARCH

COPY app.py /usr/local/bin

RUN pip install foundationdb==7.1.67
RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --shell /usr/sbin/nologin fdb

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    curl -L https://github.com/krallin/tini/releases/download/v0.19.0/tini-${TARGETARCH} -o tini-${TARGETARCH}  && \
    echo "93dcc18adc78c65a028a84799ecf8ad40c936fdfc5f2a57b1acda5a8117fa82c  tini-amd64\n07952557df20bfd2a95f9bef198b445e006171969499a1d361bd9e6f8e5e0e81  tini-arm64" > tini-sha.txt && \
    sha256sum --quiet --ignore-missing -c tini-sha.txt && \
    chmod +x tini-${TARGETARCH} && \
    mv tini-${TARGETARCH} /usr/bin/tini && \
    rm -rf /tmp/*

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENTRYPOINT [ "/usr/bin/tini", "-g", "--", "python", "/usr/local/bin/app.py" ]
