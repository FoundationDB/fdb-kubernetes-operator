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

pip install foundationdb -i https://pypi.org/project/foundationdb


