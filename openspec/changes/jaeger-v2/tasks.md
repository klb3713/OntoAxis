# Tasks: Jaeger v1 → v2 迁移

> 状态：规划产物。全部任务未开始；实施需负责人单独授权。

## 1. 版本与依赖治理

- [ ] 1.1 确认 v2 目标版本与单进程启动方式
  - 候选 `jaegertracing/jaeger:2.23.0`（2026-11-23 Docker Hub 最新非 `latest` 稳定 tag）
  - 验证：registry 查询到 tag 与 digest；`--help`/官方配置样例确认本地单进程可同时提供 OTLP 接收与 UI；确认不了则暂停并回报，不猜
- [ ] 1.2 解析并写入真实 digest，同步 `policy.yaml` / `renovate.json`
  - 验证：`make deps-lock` 后 lock 中 jaeger 条目与 registry 一致；`make deps-check` 无 `E_RENOVATE`；`review_by` 更新为迁移完成后的下次复核日

## 2. 运行时资产

- [ ] 2.1 迁移 Compose `observability` profile 的 jaeger 服务
  - 验证：`docker compose --profile observability up -d` 后 Jaeger 健康；core 请求可查询到 trace；UI 仍在 16686
- [ ] 2.2 迁移 Helm `ontoaxis-observability` 的 jaeger 组件
  - 验证：`helm template` 输出 `repo@sha256:` 且 kubeconform 通过；四种环境 overlay 渲染成功
- [ ] 2.3 替换 v1 环境变量/flag 为 v2 配置
  - 验证：沿用 v1 环境变量启动时后端不就绪且在日志指出无法识别的配置项；v2 配置启动正常

## 3. 验证 harness

- [ ] 3.1 更新 `scripts/observability-smoke.sh` 的 trace 查询断言
  - 验证：profile 启动时 smoke 退出 0 并输出 trace 查询结果；停掉 Jaeger 时 trace 相关检查失败、core 业务检查仍通过
- [ ] 3.2 负向用例：后端不可用不影响业务
  - 验证：Jaeger 停止时调用 API 返回正常状态码，日志只出现导出失败警告

## 4. 证据与文档

- [ ] 4.1 归档迁移证据到 `openspec/changes/jaeger-v2/evidence/`
  - 验证：包含 registry digest 查询、`make deps-check`、smoke 输出、v1 配置失败日志（脱敏）
- [ ] 4.2 更新文档：`docs/workflow/DEPENDENCIES.md` 删除「Jaeger v1 已 EOL」缺口条目，补 v2 说明；runbook 标注本地内存存储非持久化
  - 验证：三处文档与 policy 的 jaeger 条目一致

## 5. 回滚

- [ ] 5.1 演练回滚到 v1
  - 验证：按文档回退镜像与配置后 profile 启动、smoke 通过；记录回滚耗时与是否需要重建存储
