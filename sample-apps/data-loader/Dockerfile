FROM python:3.9-slim

COPY app.py /usr/local/bin

RUN pip install foundationdb==6.2.10
RUN groupadd --gid 4059 fdb && \
	useradd --gid 4059 --uid 4059 --shell /usr/sbin/nologin fdb

# Set to the numeric UID of fdb user to satisfy PodSecurityPolices which enforce runAsNonRoot
USER 4059

ENTRYPOINT [ "python", "/usr/local/bin/app.py" ]
