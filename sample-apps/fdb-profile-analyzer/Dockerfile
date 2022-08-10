FROM python:3.9-slim
USER root

RUN microdnf --enablerepo=ubi* --disablerepo=rhel* install -y wget \
                         bind-utils \
                         lsof  \
                         nmap-ncat \
                         less \
                         vim-minimal \
                         net-tools \
                         jq \
                         openssl \
                         pkg-config \
                         procps-ng\
                         iputils && \
     microdnf --enablerepo=ubi* --disablerepo=rhel* clean all \

RUN groupadd -g 4059 foundationdb
RUN useradd -m -d / -s /bin/bash -u 4059 -o foundationdb -g 4059 -G 266
RUN pip install foundationdb
RUN pip install dateparser
RUN rpm -ivh https://github.com/apple/foundationdb/releases/download/7.1.19/foundationdb-clients-7.1.19-1.el7.x86_64.rpm
USER foundationdb
RUN curl -O https://raw.githubusercontent.com/apple/foundationdb/main/contrib/transaction_profiling_analyzer/transaction_profiling_analyzer.py
