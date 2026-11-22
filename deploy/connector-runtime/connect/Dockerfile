# connector-runtime 本地证据 profile 的 Kafka Connect 镜像。
#
# 组成：
#   1) 构建阶段：从 Apache Iceberg 官方源码 tarball 构建 iceberg-kafka-connect runtime
#      分发件（Iceberg 未发布可直接下载的 runtime 二进制，必须自行构建）。
#   2) 运行阶段：基于固定 digest 的 Debezium Connect 镜像，把 Iceberg runtime jar 与
#      PostgreSQL JDBC 驱动放进 /kafka/connect/iceberg，供 Connect 插件路径发现。
#
# 重要（未验证）：本 Dockerfile 的完整构建没有在编写会话中执行过（任务禁止拉取大镜像、
# 禁止启动服务）。版本、URL、校验和已逐项核对，但 Gradle 构建产物路径/兼容性仍需实际
# 构建一次验证。详见 connect/README.md 的“验证状态”一节。
#
# 构建：
#   docker build \
#     --build-arg DEBEZIUM_IMAGE="quay.io/debezium/connect:3.6.1.Final@sha256:853c1f75411dea3f092254612152138c75504ab5a9f2588ecc2f9757b1d9039d" \
#     -t ontoaxis/connector-runtime-connect:3.7.1-iceberg1.12.0 \
#     deploy/connector-runtime/connect
#
# 或通过 compose（基础文件在前，保证相对路径正确）：
#   docker compose -f deploy/compose/compose.yaml \
#     -f deploy/connector-runtime/compose.connector-runtime.yaml \
#     --profile connector-runtime build kafka-connect

# ---- 全局 ARG：FROM 与构建阶段共用 -------------------------------------------
ARG DEBEZIUM_IMAGE=quay.io/debezium/connect:3.6.1.Final@sha256:853c1f75411dea3f092254612152138c75504ab5a9f2588ecc2f9757b1d9039d
ARG ICEBERG_VERSION=1.12.0
ARG ICEBERG_SOURCE_SHA512=505b4d92baf9a2b55c3893bfa1b50289a0bf0051109d81f0a92825f406c28be9096be31d5c22ed68a2d7abd39fcfb0888729bb1917216b06ac200ac0fbf11a28
ARG PGJDBC_VERSION=42.7.7
ARG PGJDBC_SHA256=157963d60ae66d607e09466e8c0cdf8087e9cb20d0159899ffca96bca2528460
ARG HADOOP_VERSION=3.4.2

# ============================================================================
# 阶段 1：构建 Iceberg Kafka Connect runtime
# ============================================================================
FROM eclipse-temurin:21-jdk@sha256:8ad118b146aa92da815b576441acd6b42b38236797ad0b6edc8c200b85bb0734 AS iceberg-builder

ARG ICEBERG_VERSION
ARG ICEBERG_SOURCE_SHA512
ARG PGJDBC_VERSION
ARG PGJDBC_SHA256
ARG HADOOP_VERSION

SHELL ["/bin/bash", "-euxo", "pipefail", "-c"]

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl tar gzip unzip \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# 源码 tarball 与 SHA512 均来自官方 releases/archive（downloads.apache.org 为 Apache 官方
# 发布镜像）。SHA512 已在编写会话中下载并核对；如上游撤回/重发，构建会因校验失败而中止。
RUN curl -fsSL --retry 3 --retry-delay 2 \
        -o /build/apache-iceberg-${ICEBERG_VERSION}.tar.gz \
        "https://downloads.apache.org/iceberg/apache-iceberg-${ICEBERG_VERSION}/apache-iceberg-${ICEBERG_VERSION}.tar.gz" \
 && echo "${ICEBERG_SOURCE_SHA512}  /build/apache-iceberg-${ICEBERG_VERSION}.tar.gz" | sha512sum -c - \
 && mkdir -p /build/src /build/dist /build/iceberg-libs \
 && tar -xzf /build/apache-iceberg-${ICEBERG_VERSION}.tar.gz -C /build/src --strip-components=1

WORKDIR /build/src

# Gradle wrapper 版本来自源码树（8.14.5，wrapper 自带 distributionSha256Sum）。
# 只构建 runtime 分发件；跳过测试与集成测试以缩短构建时间。
# 注意：项目路径与产物目录可能随 Iceberg 版本调整，下面的 find 做了兼容处理。
RUN ./gradlew --no-daemon --stacktrace \
        -x test -x integrationTest \
        :iceberg-kafka-connect:iceberg-kafka-connect-runtime:distZip \
 && ZIP="$(find /build/src -type f -path '*/distributions/*.zip' ! -iname '*hive*' | head -n 1)" \
 && if [ -z "$ZIP" ]; then ZIP="$(find /build/src -type f -path '*/distributions/*.zip' | head -n 1)"; fi \
 && test -n "$ZIP" \
 && echo "using distribution: $ZIP" \
 && unzip -q "$ZIP" -d /build/dist \
 && find /build/dist -type f -path '*/lib/*.jar' -exec cp -t /build/iceberg-libs {} + \
 && ls -1 /build/iceberg-libs | wc -l

# PostgreSQL JDBC 驱动：Iceberg JdbcCatalog 与 Trino JDBC catalog 都需要它。
# Maven Central 只发布 .sha1，没有 .sha256；这里的 SHA256 是在编写会话中下载 jar 后
# 本地计算的结果，并与官方 .sha1 交叉核对，见 connect/README.md。
RUN curl -fsSL --retry 3 --retry-delay 2 \
        -o /build/postgresql-${PGJDBC_VERSION}.jar \
        "https://repo1.maven.org/maven2/org/postgresql/postgresql/${PGJDBC_VERSION}/postgresql-${PGJDBC_VERSION}.jar" \
 && echo "${PGJDBC_SHA256}  /build/postgresql-${PGJDBC_VERSION}.jar" | sha256sum -c - \
 && cp /build/postgresql-${PGJDBC_VERSION}.jar /build/iceberg-libs/
# Iceberg 1.12 的 runtime 分发件不包含 commons-configuration2（由宿主应用的
# 隔离类加载器不可见），显式补入插件目录。
RUN curl -fsSL --retry 3 --retry-delay 2 \
        -o /build/iceberg-libs/commons-configuration2-2.12.0.jar \
        "https://repo1.maven.org/maven2/org/apache/commons/commons-configuration2/2.12.0/commons-configuration2-2.12.0.jar" \
 && curl -fsSL --retry 3 --retry-delay 2 \
        -o /build/iceberg-libs/commons-lang3-3.18.0.jar \
        "https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/3.18.0/commons-lang3-3.18.0.jar" \
 && curl -fsSL --retry 3 --retry-delay 2 \
        -o /build/iceberg-libs/commons-text-1.13.0.jar \
        "https://repo1.maven.org/maven2/org/apache/commons/commons-text/1.13.0/commons-text-1.13.0.jar" \
 && for jar in hadoop-client-api hadoop-client-runtime; do \
      curl -fsSL --retry 3 --retry-delay 2 -o /build/iceberg-libs/${jar}-${HADOOP_VERSION}.jar \
        "https://repo1.maven.org/maven2/org/apache/hadoop/${jar}/${HADOOP_VERSION}/${jar}-${HADOOP_VERSION}.jar"; \
      curl -fsSL --retry 3 --retry-delay 2 -o /tmp/${jar}.sha1 \
        "https://repo1.maven.org/maven2/org/apache/hadoop/${jar}/${HADOOP_VERSION}/${jar}-${HADOOP_VERSION}.jar.sha1"; \
      echo "$(cat /tmp/${jar}.sha1)  /build/iceberg-libs/${jar}-${HADOOP_VERSION}.jar" | sha1sum -c -; \
    done


# ============================================================================
# 阶段 2：运行镜像（固定 digest 的 Debezium Connect）
# ============================================================================
FROM ${DEBEZIUM_IMAGE}

LABEL org.opencontainers.image.title="ontoaxis connector-runtime connect" \
      org.opencontainers.image.description="Debezium Connect 3.6.1.Final + Apache Iceberg Kafka Connect sink 1.12.0 (local evidence profile, not production)" \
      org.opencontainers.image.source="https://github.com/klb3713/OntoAxis" \
      org.opencontainers.image.licenses="Apache-2.0"

# 基础镜像以 kafka 用户运行；插件目录属主保持 kafka。
COPY --from=iceberg-builder --chown=kafka:kafka /build/iceberg-libs/ /kafka/connect/iceberg/

# 插件路径同时包含 Debezium 自带插件、Iceberg 插件、可选外部插件卷。
ENV CONNECT_PLUGIN_PATH="/kafka/connect,/kafka/connect/iceberg,/kafka/connect/extra-plugins"

USER kafka
WORKDIR /kafka

# 由 compose 统一提供 healthcheck/端口/环境变量。
CMD ["start"]
