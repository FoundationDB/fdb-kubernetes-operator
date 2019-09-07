FROM python:3.6

WORKDIR /var/fdb/tmp
ARG FDB_VERSION=6.1.8
ARG FDB_LIBRARY_VERSIONS="6.1.8 6.0.18 5.2.8 5.1.7"
ARG FDB_WEBSITE=https://www.foundationdb.org

ADD website /mnt/website
RUN curl $FDB_WEBSITE/downloads/$FDB_VERSION/linux/fdb_$FDB_VERSION.tar.gz -o fdb_$FDB_VERSION.tar.gz && \
	tar -xzf fdb_$FDB_VERSION.tar.gz --strip-components=1 && \
	rm fdb_$FDB_VERSION.tar.gz && \
	chmod u+x fdbbackup fdbcli fdbdr fdbmonitor fdbrestore fdbserver backup_agent dr_agent && \
	mv fdbbackup fdbcli fdbdr fdbmonitor fdbrestore fdbserver backup_agent dr_agent /usr/bin && \
	echo ${FDB_VERSION} > /var/fdb/version && mkdir -p /var/fdb/lib && \
	for version in $FDB_LIBRARY_VERSIONS; do curl $FDB_WEBSITE/downloads/$version/linux/libfdb_c_$version.so -o /var/fdb/lib/libfdb_c_${version%.*}.so; done && \
	rm -r /var/fdb/tmp

WORKDIR /

COPY sidecar.py /
COPY entrypoint.bash /

ENV COPY_ONCE 0
ENV SIDECAR_CONF_DIR /var/sidecar-conf
RUN mkdir $SIDECAR_CONF_DIR
COPY default_config.json $SIDECAR_CONF_DIR/config.json

VOLUME /var/input-files
VOLUME /var/output-files

ENV LISTEN_PORT 8080
ENTRYPOINT ["/entrypoint.bash"]

