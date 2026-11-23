#!/usr/bin/env bash
# deps.sh — 依赖版本治理入口。实际逻辑在 scripts/deps.py（python3 标准库）。
#
# 用法：
#   scripts/deps.sh lock [--write]   生成锁文件
#   scripts/deps.sh check            离线校验（CI 阻塞项）
#   scripts/deps.sh audit [--out P]  联网审计，输出 markdown
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "${ROOT}/scripts/deps.py" "$@"