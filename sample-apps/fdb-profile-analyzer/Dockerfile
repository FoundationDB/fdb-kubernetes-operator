FROM python:3.9-slim
USER root

ARG FDB_VERSION=7.1.33
RUN apt-get update \
    && apt-get  install -y wget \
                         bind9-utils \
                         lsof  \
                         less \
                         net-tools \
                         jq \
                         openssl \
                         pkg-config

RUN groupadd -g 4059 foundationdb
RUN useradd -m -d / -s /bin/bash -u 4059 -o foundationdb -g 4059
RUN mkdir /app/
RUN chown foundationdb:foundationdb -R /app/
RUN pip install foundationdb==${FDB_VERSION}
RUN pip install dateparser==1.1.1
RUN wget https://github.com/apple/foundationdb/releases/download/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_amd64.deb && \
    dpkg -i foundationdb-clients_${FDB_VERSION}-1_amd64.deb
WORKDIR /app/
USER foundationdb
RUN wget https://raw.githubusercontent.com/apple/foundationdb/${FDB_VERSION}/contrib/transaction_profiling_analyzer/transaction_profiling_analyzer.py
