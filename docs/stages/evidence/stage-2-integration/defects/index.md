# Stage 2 集成缺陷登记（2026-11-23）

| id | 来源 | 分类 | 描述 | 状态 | 处置/链接 |
|---|---|---|---|---|---|
| D-01 | 2.2 edge-gateway | 跨 change 装配缺陷 | `compose.authz.yaml` 未给 `ontoaxis-core-edge` 注入 authz 环境，edge core 启动失败 | 已修复 | 增加 core-edge authz env；edge smoke 12/0；见 `../compose-smoke.md` |
| D-02 | 2.5 pipeline-kestra-dbt | 依赖缺失 | 缺少 Kestra profile/服务与 dbt 执行镜像的 Compose 片段，无法启动 E2E | 待处置 | 归属 pipeline-kestra-dbt 的 4.5；需 owner 合并 compose |
| D-03 | 2.4 connector-runtime | 环境/未验证 | Kafka Connect/Debezium profile 仅 YAML 校验，未起容器/未跑 CDC E2E | 待处置 | 见 `deploy/connector-runtime/README.md`；需受控环境 |
| D-04 | 2.7 object-search-meilisearch | 依赖缺失 | 缺少 Meilisearch Compose profile 与 worker 合并；仅单元/DB-gated 测试 | 待处置 | 需 search profile + worker 合并 |
| D-05 | 2.8 production-delivery | 环境/未验证 | Helm/ArgoCD 静态渲染通过；kind/k3d 安装、备份恢复、回滚未在集群执行 | 待处置 | 需受控集群环境 |
| D-06 | 2.1/2.3 | 环境问题 | 本地 core 依赖 authz overlay；基础 compose 单文件启动会因缺少 authz 环境失败 | 已说明 | `compose-smoke.md`、`app-wiring.md` |


## D-03 补充（connector CDC，2026-11-23）
- 已把 Debezium 基础镜像切到 3.6.1（Java 21），source/sink task 可 RUNNING，Kafka topic 有 snapshot 记录。
- Kafka Connect Iceberg 1.12.0 sink 仍不提交：consumer group Stable 但 lag 不降；metadata 只有 `00000-*.json`、`snapshots=[]`。
- 已尝试静态表路由、RegexRouter SMT、动态表路由（缺 route-field 失败）、独立 control topic/group；均未解决。
- 建议：降级 Iceberg Connect 到已验证版本或改用 JDBC sink 完成本地 E2E；当前不勾选 5.3/6.2。


## D-02 补充（pipeline Kestra，2026-11-23）
- 尝试拉取 `kestra/kestra:latest` 做本地 profile 探针，镜像拉取超过 2 分钟未完成，已取消并清理容器。
- pipeline-kestra-dbt 的 compose profile / 真实 Kestra E2E 仍阻塞在镜像体积与 dbt worker 镜像构建；4.5/5.3/6.1/6.2 未完成。
- HTTP adapter 端点已对照设计（`/api/v1/flows/{ns}/{id}`、`/api/v1/namespaces/{ns}/files`、`/api/v1/executions/{ns}/{flow}`），待 Kestra 版本确认。


### D-03 追加：sink 主题前缀剥离候选修复（2026-11-23）
- `BuildSinkConfig` 增加 `RegexRouter` SMT：`transforms=stripTopicPrefix`，regex `^<topic_prefix>\.`，replacement 空，使 sink 记录 topic 变为 `public.<table>`，匹配 `iceberg.tables=public.<table>`；broker 主题与租户隔离不变。
- 实测：加 `RegexRouter` SMT 后 sink task 仍 RUNNING，但 consumer lag 不下降、Iceberg metadata 未新增 snapshot；已回退 `sink.go`/`sink_test.go` 到 baseline（单元测试通过）。真实 Debezium→Iceberg 三向行数 E2E 仍未通过，5.3/6.2 保持未勾选。

### D-02 追加：Kestra v0.24 本地 E2E 启动结论（2026-11-23）
- `deploy/compose/pipeline-kestra/Dockerfile` 的 `pip3 install --break-system-packages` 在 Ubuntu 22.04 的 pip 上不存在，已改为 `pip3 install`；镜像构建成功。
- compose healthcheck 改为 `GET /api/v1/configs`（`/api/v1/flows` 对匿名返回 401，导致容器 unhealthy）。
- Kestra 0.24.0 API 对 `/api/v1/flows` 返回 `401 WWW-Authenticate: Basic`，即使 `kestra.server.basic-auth.enabled: false`；`HTTPAdapter` 目前只发 `Authorization: Bearer`，publish 返回 503 `unavailable`。
- 需在 Kestra 侧创建 API token/用户或让适配器支持 Basic 认证；在此之前 pipeline 13.x 真实 E2E 不勾选。

### D-02 追加：Kestra Basic 认证尝试（2026-11-23）
- 已为 `HTTPAdapter` 增加 `basic:` token 前缀支持（发送 `Authorization: Basic base64(user:pass)`），并增加单测。
- 但 Kestra 0.24.0 容器在 `kestra.server.basic-auth.enabled: true` + 自定义 username/password、以及默认 `admin@kestra.io` 组合下仍返回 401；`KESTRA_SERVER_BASIC_AUTH_ENABLED=true` 同样无效。
- 结论：Kestra 0.24 OSS 镜像的认证路径未确认，需要查其实际用户/令牌初始化方式，或改用可匿名访问的旧版本/测试镜像；pipeline 13.x 真实 E2E 继续不勾选。

### D-02 追加：Kestra 0.17 探测（2026-11-23）
- `kestra/kestra:v0.17.0` 本地启动后 `/api/v1/configs` 200，`/api/v1/flows` GET 405（该版本列表接口不是 GET root），说明认证本身未阻塞旧版本。
- 0.24 的 `/api/v1/flows` 401 可能是该版本新增的 API 认证过滤器；适配器实际使用 `PUT /api/v1/flows/{namespace}/{id}`、`POST /api/v1/executions/...`，需要在 0.24 上用真实 PUT/Trigger 验证，而不是用 GET 列表判断。
- 下一步：在 0.24 容器内直接调用适配器的 PUT flow / trigger execution 端点；若仍 401，再降级到 0.17 或改 Kestra 认证配置。
