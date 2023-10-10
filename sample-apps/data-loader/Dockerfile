FROM python:3.9-slim

COPY app.py /usr/local/bin

RUN pip install foundationdb==6.2.10
RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --shell /usr/sbin/nologin fdb

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    curl -Ls https://github.com/krallin/tini/releases/download/v0.19.0/tini-amd64 -o tini  && \
    echo "93dcc18adc78c65a028a84799ecf8ad40c936fdfc5f2a57b1acda5a8117fa82c  tini" > tini-sha.txt && \
    sha256sum --quiet -c tini-sha.txt && \
    chmod +x tini && \
    mv tini /usr/bin/ && \
    rm -rf /tmp/*

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENTRYPOINT [ "/usr/bin/tini", "-g", "--", "python", "/usr/local/bin/app.py" ]
