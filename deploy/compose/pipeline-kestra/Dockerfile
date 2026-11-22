FROM kestra/kestra:v0.24.0
USER root
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-pip curl && \
    pip3 install dbt-trino==1.8.0 && \
    rm -rf /var/lib/apt/lists/*
USER kestra
