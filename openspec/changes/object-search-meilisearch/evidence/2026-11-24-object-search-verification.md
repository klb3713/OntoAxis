# 2026-11-24 对象搜索隔离验证

## 范围

仅本机隔离环境与合成数据。本记录用于 `object-search-meilisearch` 的变更验证，不是生产验收、Stage G2、CI 或评审签核。不改已有容器、数据库、外部 IdP、共享交付配置。G1 事实见 [Stage 2 G1 记录](../../../../docs/stages/evidence/stage-2-g1-gate-record.md)。

## 1. 真实 PostgreSQL、Meilisearch 与 CLI

- PostgreSQL 16.14 和官方 Meilisearch 1.11.3 进程共用本轮私有临时目录；`lsof` 核对均只监听 `127.0.0.1` 的临时端口。使用合成数据库账号、测试索引密钥和自动生成的租户/对象；Meilisearch 关闭遥测。验证完成后两个进程均停止。
- 命令：为该回环环境设置 `ONTOAXIS_SEARCH_TEST_DATABASE_URL`、`ONTOAXIS_SEARCH_TEST_MEILISEARCH_URL`、`ONTOAXIS_SEARCH_TEST_API_KEY` 和 `ONTOAXIS_SEARCH_TEST_REQUIRED=1` 后，执行 `go test -tags=integration -p 1 -count=1 -timeout 10m -v ./internal/platform/search/... ./cmd/ontoaxis-search`，退出 0，**12 项真实后端集成测试通过，无跳过**。
- `TestIntegrationRebuildKeysetPagination`：1,100 个对象，验证跨扫描批次和索引文档分页，无遗漏或重复。`TestIntegrationRebuildVerifyDetectsDrift`：一致通过；等数量内容变更被摘要识别；删除另一路径报告数量差异。`TestIntegrationRebuildSummaryStable`：25 个对象，两次重建文档数和 SHA-256 摘要一致。
- `TestIntegrationRebuildCLIExitAndDigest`：编译并执行真实 `ontoaxis-search rebuild --tenant <合成租户> --verify`。两次执行均退出 `0`，分别返回 `verified=true`、同一索引、相同文档数和 `digest`。第三次在交换索引后、对账读取前经本机回环代理修改真实 Meilisearch 的一条文档；文档数量不变，CLI 退出 `1`，报告 `document digest mismatch` 和库/索引两个摘要，不打印被修改的文档内容。代理只控制注入时点，不伪造后端响应。
- 其余覆盖：迁移 up/down/up，消费者并发认领和增删改收敛，整租户原子切换、窗口事件重放、失败清理、单类型保留其他类型、租户索引隔离。这不是 HTTP/认证整栈测试；单类型仍是原地修复，不按原子切换取证。

## 2. 授权边界与常规检查

- `TestSearchObjectTypeAuthorizationUsesTenantFacts`：同租户类型允许/拒绝、未知/跨租户/混合类型返回 403；事实读端缺失/异常/不匹配、守卫超时返回 503；拒绝和失败均不访问索引，不暴露 `items` / `total`。使用实际处理器、Guard 和 Cedar 评估器；OpenFGA 关系查询是测试替身，不能替代真 OpenFGA 运行证据。
- `go build ./...`、`go vet ./...`、`go test -count=1 ./...`、`go test -race -count=1 ./...` 均退出 0；未增加 Go 依赖。

## 3. 中文合成检索基线

- 同一 Meilisearch 实例运行 `bash scripts/search-quality-baseline.sh`，退出 0。24 条合成文档、6 次查询，`recall@5=1.0000`、`MRR@10=1.0000`。
- 草案阈值分别为 0.9 / 0.8，仍为 `pending-owner-signoff`；仅报告技术结果，不判业务合格。
- 工程回归门（2026-11-24 负责人签核）：`engineering_gate` 冻结语料 `corpus_sha256=32b8e81f4da20982f02acaf31be950c70f4f6472499a010f3ccb2ac5ba922b2f`、基线 `recall@5=1.00`/`MRR@10=1.00`。用临时 Meilisearch 1.11 隔离容器（仅 `127.0.0.1`）实跑 `scripts/search-quality-baseline.sh --strict`，退出 0、工程门 `PASS`（`corpus_sha256_match=True`）；负例（语料哈希改错、度量降到基线下）均 `FAIL` 且 `--strict` 退出 3，未签核门输出 `UNSIGNED`。业务阈值仍 `pending-owner-signoff`，只作信息性打印。

## 4. Compose 整栈与交接

- `bash -n scripts/object-search-e2e.sh` 与 `python3 -B scripts/object-search-e2e-test.py` 退出 0（10 项离线回归）；真实 Compose 渲染通过预检：四个发布端口仅绑定 `127.0.0.1`，PostgreSQL 不发布端口，不读取项目 `.env`，关闭遥测外发。
- 首次尝试（2026-11-24 13:0x）：`E2E_START_TIMEOUT=150 bash scripts/object-search-e2e.sh`，项目 `ontoaxis-search-e2e-ad5393eeb8d2b674c87719d4`。预检通过，core/worker 镜像构建成功，七个容器均创建；PostgreSQL、Keycloak、OpenFGA、Meilisearch 停在 `Starting`，检查时七个容器均为 `Created`，脚本 150 秒限时退出码 1。事后诊断：本机 Docker 可 `create`、可 `exec` 进既有容器，但任何新容器 `start` 无事件且长时间不返回（磁盘 404G 可用、内存 32G、无守护进程错误日志），属本机 Docker Desktop 运行时故障，非脚本或应用缺陷。
- 环境恢复：经负责人同意后执行 `docker desktop restart`（约 52 秒），守护进程恢复；最小容器启动探测 `docker run --rm alpine:3.20 echo start-ok` 1.0 秒退出 0。随后 `docker compose -p ontoaxis-local -f compose.yaml -f authz/compose.authz.yaml up -d` 恢复既有栈：postgres、keycloak、openfga、trino、ontoaxis-core、web 均 healthy，`/healthz` 与 web 首页返回 200；`geoqc-dev` 与其 kind 集群由重启策略自动恢复，`ontoaxis-registry-rehearsal` 手动 `start` 恢复。未删除任何卷或既有数据。
- 复跑（2026-11-24 21:07）：`E2E_PROJECT=ontoaxis-search-e2e-a57bd15f8187 E2E_START_TIMEOUT=300 bash scripts/object-search-e2e.sh`，退出 0，输出 `object-search e2e: 73 passed, 0 failed`。`search` profile 七个容器在隔离项目内真实启动，四个发布端口仅绑 `127.0.0.1`（56804–56807）。矩阵覆盖 create→search→update→search→delete→search、租户 A/B 隔离、两次连续重建 `--verify` 退出 0 且文档数与 `digest` 一致（A：`19ec9283…2b7da8`）、OpenFGA 暂停时搜索 503（授权不可用）与恢复、Meilisearch 停机时搜索可重试 503 且 core `/healthz`、`/readyz`、worker 存活保持 200、worker 就绪 503 并指认 `search`、重启后消费者自动追平（outbox 待发 0）、worker `/metrics` 暴露非零 index-lag 直方图。
- 清理核验：隔离项目容器、网络、卷均无残留；本轮仅保留该项目两个构建镜像标签 `ontoaxis-search-e2e-a57bd15f8187-ontoaxis-core:latest`、`…-search-worker:latest`，已按标签删除。主 `compose.yaml` 未改，`search` profile 片段合并仍归 `production-delivery`（7.5）。

## 5. 原生进程 HTTP 功能矩阵（非 Compose）

- Compose 启动超时后，在另一独占临时目录使用本机 PostgreSQL 16.14、Meilisearch 1.11.3、Keycloak 26.0.8、OpenFGA 1.23.0 和当前工作树构建的 core/worker。加载同一 realm 合成夹具、OpenFGA 模型及 Cedar 策略；检查所有 TCP 监听均为 `127.0.0.1`。本轮运行复用 `scripts/object-search-e2e.sh` 的业务断言，把 Compose 启停、CLI 调用和 outbox 查询替换为独占进程操作、实际 CLI 和真实 PostgreSQL 查询。
- 最终输出 `object-search native HTTP: 68 passed, 0 failed`，进程清理退出 `0`；核对无残留进程，移除本轮临时数据、合成凭据和临时脚本。三位合成用户由 Keycloak 实际签发。授权搜索返回 200 并带决策 ID；无 token 返回 401；跨租户、混合类型及无读权限返回 403；暂停 OpenFGA 返回 503（Meilisearch 仍健康），无 `items` / `total`，恢复后搜索成功。
- 两个租户的创建与搜索相互隔离；更新后命中新名称且不命中旧名称，删除后不再命中。直接查询索引中合成敏感属性值为 0 命中。连续两次重建使用真实 CLI `--verify`，A 的文档数、索引和摘要一致。该属性用例不代表所有敏感数据被排除。
- 停止 Meilisearch 后，搜索返回可重试 503；core 健康/就绪和 worker 存活保持 200，worker 就绪返回 503；core 仍能创建、读取、更新对象。重启后 worker 恢复就绪，自动追平，不需手动重放；两个合成租户 outbox 待发为 0。消费者滞后直方图共 7 个观察值，6 个不超过 0.5 秒、7 个不超过 1 秒；这是功能观测，不是 HTTP p95 或 SLO 验收。
- 这次运行不证明 `search` profile、Helm/kind/ArgoCD、外部 IdP 或 G2 通过；8.1 仍需在可用 Docker 上执行仓库脚本。

## 6. 待交接与决策

- `production-delivery`：主 Compose 合并和健康判定、Helm/ArgoCD 与 kind 部署审核仍由交付负责人完成；不以隔离脚本替代正式部署。
- `stage-2-integration`：本记录可供收录；尚未写入共享 Stage 证据目录，CI run 链接待补。
- 负责人评审：单类型原地重建与原子语义的边界、可索引字段/敏感属性、数据保留和搜索质量阈值仍需决定；运维与安全评审未签。
