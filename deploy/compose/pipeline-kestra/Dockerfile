FROM kestra/kestra:v2.0.12@sha256:b521aff2847161c283450d3c224523b42ea97447d86bde9519576c393d914b76
USER root
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-pip curl && \
    pip3 install --break-system-packages dbt-trino==1.8.0 && \
    rm -rf /var/lib/apt/lists/*
USER kestra
