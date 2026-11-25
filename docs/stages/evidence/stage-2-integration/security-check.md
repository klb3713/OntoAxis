# 9.2 证据边界与安全检查（2026-11-25）

- 环境：本地 macOS，工作树基于主干 `1de309d`。
- 命令：`docker run --rm -v "$PWD:/repo" ghcr.io/gitleaks/gitleaks:v8.24.3 detect --no-git --source=/repo --config=/repo/.gitleaks.toml --report-format json`
- 结果：22 条命中，分布在 13 个文件；其中 12 个文件是**未跟踪**产物（`deploy/kind/tls/key.pem`、`dist/delivery/**` 等，`git ls-files` 为空），不入库；
  唯一已跟踪文件命中是 `deploy/kind/bootstrap.sh:199` 的 `generic-api-key`，值为 `condition=Available`——`kubectl wait --for=condition=Available` 的误报（README/.gitleaks.toml 已登记该语义）。
- 差异说明：本地 gitleaks v8.24.3 CLI 对 `.gitleaks.toml` 的 `[allowlist].regexes` 默认按 `secret` 目标匹配，未抑制该误报；将其 `regexTarget = "match"` 后可抑制（实测 21 条、无 bootstrap.sh）。CI `secret-scan`（主干 run `45543404516`）为绿色。是否在仓库配置中加 `regexTarget = "match"` 属安全边界调整，留给负责人决定，本轮未改 `.gitleaks.toml`。
- 其余检查：`make license-check` 通过（`LICENSE_OK`，`nexus-proto-annotations` 按既有批准路径例外）；证据文件中无 token/密码/生产连接串；`.env` 未提交。
- 边界：Stage 2 证据均为本地/合成数据验证；生产验收由交付环境执行，本文件不把本地结果写成 CI/生产通过。
