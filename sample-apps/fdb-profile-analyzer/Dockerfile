FROM python:3.9-slim
USER root

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
RUN pip install foundationdb==7.1.21
RUN pip install dateparser==1.1.1
RUN wget https://github.com/apple/foundationdb/releases/download/7.1.19/foundationdb-clients_7.1.19-1_amd64.deb && \
    dpkg -i foundationdb-clients_7.1.19-1_amd64.deb
WORKDIR /app/
USER foundationdb
RUN wget https://raw.githubusercontent.com/apple/foundationdb/7.1.21/contrib/transaction_profiling_analyzer/transaction_profiling_analyzer.py
