# tag → digest 映射

- 生成时间：2026-11-23T16:53:13+0800
- 来源：`deploy/deps/lock.yaml` + Docker Hub / Quay manifest 查询
- 说明：digest 为 registry 返回的 manifest（多架构为 image index）digest；查询失败记 unknown，不推测。

| 组件 | 镜像 | tag | digest | 来源 |
|---|---|---|---|---|
| alpine | `alpine` | `3.20` | `sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc` | deploy/compose/action-temporal/Dockerfile,deploy/compose/search/Dockerfile |
| buf | `bufbuild/buf` | `1.75.0` | `sha256:adf82c7170be6629ec81dc907921ac1699407cd11c4f7fd7521b13c74152f849` | Makefile |
| authz-init | `curlimages/curl` | `8.10.1` | `sha256:d9b4541e214bcd85196d6e92e2753ac6d0ea699f0af5741f8c6cccbfcf00ef4b` | deploy/compose/authz/compose.authz.yaml |
| eclipse-temurin | `eclipse-temurin` | `21-jdk` | `sha256:8ad118b146aa92da815b576441acd6b42b38236797ad0b6edc8c200b85bb0734` | deploy/connector-runtime/connect/Dockerfile |
| meilisearch | `getmeili/meilisearch` | `v1.11` | `sha256:6f8f1fc29d1ff67b4d8d503bb4e0a69b55c8d42600decafde951344218193a27` | deploy/compose/search/compose.search.yaml |
| golang | `golang` | `1.26-alpine` | `sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab` | deploy/compose/action-temporal/Dockerfile,deploy/compose/search/Dockerfile |
| golang | `golang` | `1.26.3` | `sha256:2d6c80227255c3112a4d08e67ba98e58efd3846daf15d9d7d4c389565d881b1a` | Makefile |
| jaeger | `jaegertracing/all-in-one` | `1.62.0` | `sha256:836e9b69c88afbedf7683ea7162e179de63b1f981662e83f5ebb68badadc710f` | deploy/compose/compose.yaml |
| kestra | `kestra/kestra` | `v2.0.12` | `sha256:b521aff2847161c283450d3c224523b42ea97447d86bde9519576c393d914b76` | deploy/compose/pipeline-kestra/Dockerfile |
| openfga | `openfga/openfga` | `v1.23.0` | `sha256:2a1b4ef95f2f27e6d420f11cf14390b4f537e699de8d18b6a07d8b355e11cf02` | deploy/compose/authz/compose.authz.yaml |
| otel-collector | `otel/opentelemetry-collector-contrib` | `0.120.0` | `sha256:85ac41c2db88d0df9bd6145e608a3cb023f5d8443868adbfbbf66efb51087917` | deploy/compose/compose.yaml |
| perses | `persesdev/perses` | `v0.50.0` | `sha256:07f3f93f8e52089f8d5373e9da831b645ff3a80aac67507b35b7582c5bd60924` | deploy/compose/compose.yaml |
| postgres | `postgres` | `16-alpine` | `sha256:f2f98726820d0342055351c6e9d36e705f5bfffb3dd53ff556325114b08afb6d` | deploy/compose/compose.yaml |
| alertmanager | `prom/alertmanager` | `v0.27.0` | `sha256:e13b6ed5cb929eeaee733479dce55e10eb3bc2e9c4586c705a4e8da41e5eacf5` | deploy/compose/compose.yaml |
| prometheus | `prom/prometheus` | `v3.1.0` | `sha256:6559acbd5d770b15bb3c954629ce190ac3cbbdb2b7f1c30f0385c4e05104e218` | Makefile,deploy/compose/compose.yaml |
| keycloak | `quay.io/keycloak/keycloak` | `26.0` | `sha256:09a381c715ab0b111835b70f2905955274843a219c6f27efb348e4d9f4086858` | deploy/compose/compose.yaml |
| temporal-init | `temporalio/admin-tools` | `1.26.2` | `sha256:37e2e33dbd7b86a12fa1daecf48f5b42544956ae503a83bd45460587f25591f2` | deploy/compose/action-temporal/compose.temporal.yaml |
| temporal | `temporalio/auto-setup` | `1.26.2` | `sha256:e9245820522233ec6a59ad69c6512ae4450705ee02bf6894fc61ba13b238c1c0` | deploy/compose/action-temporal/compose.temporal.yaml |
| temporal-ui | `temporalio/ui` | `2.31.2` | `sha256:28bb3ea5a6ea3e09f16b521f32ab727c96470f7f1e420c66a6cbfb02001a8aa2` | deploy/compose/action-temporal/compose.temporal.yaml |
| oa-edge | `traefik` | `v3.3` | `sha256:2cd5cc75530c8d07ae0587c743d23eb30cae2436d07017a5ff78498b1a43d09f` | deploy/compose/compose.yaml |
| trino | `trinodb/trino` | `484` | `sha256:2704eb53935af5e582d63d6033e9bf99d20c325e9b0e30b1016ef59a4773979f` | deploy/compose/compose.yaml |

共 21 条，unknown 0 条。

## 附录：覆盖范围与缺口（人工补充，非生成内容）

上表由 `python3 scripts/deps.py resolve` 生成，覆盖 lock 中**带 tag** 的引用（21 条），
其中 12 个 Helm 组件能从 compose/Dockerfile/Makefile 找到对应 tag，可直接用于回填。

以下 9 个组件在 lock 中没有任何 tag，无法机械解析，需先做版本决策：

| 组件 | 原因 | 下一步 |
| --- | --- | --- |
| `ontoaxis-core`、`web`、`ontoaxis-search` | 自有镜像，由 CI 构建，tag 是构建产物的 git short SHA | digest 从 CI 推送结果取，不查上游；回填时写 `repository` + 该次构建的 digest + tag=commit |
| `apicurio`、`debezium`、`kafka`、`nessie`、`seaweedfs`、`valkey` | 只出现在 Helm values，仓库内没有对应 tag | 先由 `production-delivery` 定版本（属台账 6.3 的版本决策），再用 `--ref` 解析 |

用法（Helm-only 组件定好版本后）：

```bash
python3 scripts/deps.py resolve --ref docker.io/apache/kafka:4.4.0 --ref chrislusf/seaweedfs:3.80
```

注意：

- 本表是**准备材料，不是证据**；回填后以 lock、`helm template` 输出和 `make deps-check` 为准。
- digest 为 registry 返回的 manifest（多架构为 image index）digest，与 Helm `repository + digest`
  的渲染形式一致；单架构镜像的 digest 可能与 index 不同，回填时以 `helm template` 实际输出核对。
- 表内版本是仓库**当前**引用的版本，不等于 6.3 的升级目标；若先升级再回填，需重新生成。
