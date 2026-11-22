#!/usr/bin/env bash
# 下载 Trino Iceberg 插件所需的 PostgreSQL JDBC 驱动（可复用同一 jar 给 Connect）。
#
# 为什么需要：stock trinodb/trino 镜像的 /usr/lib/trino/plugin/iceberg/ 目录只包含
# Iceberg catalog 依赖，不保证包含 PostgreSQL JDBC 驱动；Trino 的 JDBC catalog 通过
# DriverManager 连接 Postgres，驱动必须位于插件 classpath。
#
# 用法：
#   ./deploy/connector-runtime/trino/fetch-plugin.sh
#   ./deploy/connector-runtime/trino/fetch-plugin.sh --check   # 只校验已下载文件
#
# 校验：Maven Central 只发布 .sha1（没有 .sha256），因此 SHA256 是本仓库在编写会话中
# 下载后本地计算并记录的值，并与官方 .sha1 交叉核对；见 connect/README.md 的版本表。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
DEST_DIR="${ROOT}/deploy/connector-runtime/trino/plugins"
VERSION="42.7.7"
JAR="postgresql-${VERSION}.jar"
URL="https://repo1.maven.org/maven2/org/postgresql/postgresql/${VERSION}/${JAR}"
SHA256="157963d60ae66d607e09466e8c0cdf8087e9cb20d0159899ffca96bca2528460"

usage() {
  sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

CHECK_ONLY=0
case "${1:-}" in
  "") ;;
  --check) CHECK_ONLY=1 ;;
  -h|--help) usage; exit 0 ;;
  *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
esac

command -v curl >/dev/null 2>&1 || { echo "FAIL: 需要 curl" >&2; exit 1; }
if command -v shasum >/dev/null 2>&1; then
  SHA_CMD=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD=(sha256sum)
else
  echo "FAIL: 需要 shasum -a 256 或 sha256sum" >&2
  exit 1
fi

mkdir -p "${DEST_DIR}"

verify() {
  local file="$1"
  local got
  got="$("${SHA_CMD[@]}" "${file}" | awk '{print $1}')"
  if [[ "${got}" != "${SHA256}" ]]; then
    echo "FAIL: SHA256 不匹配: ${file}" >&2
    echo "  期望: ${SHA256}" >&2
    echo "  实际: ${got}" >&2
    return 1
  fi
  echo "PASS: SHA256 校验通过: ${file}"
}

if [[ "${CHECK_ONLY}" -eq 1 ]]; then
  if [[ ! -f "${DEST_DIR}/${JAR}" ]]; then
    echo "FAIL: 未找到 ${DEST_DIR}/${JAR}，请先运行本脚本（不带 --check）" >&2
    exit 1
  fi
  verify "${DEST_DIR}/${JAR}"
  exit 0
fi

if [[ -f "${DEST_DIR}/${JAR}" ]]; then
  echo "已存在 ${JAR}，重新校验..."
  verify "${DEST_DIR}/${JAR}" && exit 0
  echo "校验失败，重新下载..." >&2
  rm -f "${DEST_DIR}/${JAR}"
fi

echo "下载 ${URL}"
curl -fL --retry 3 --retry-delay 2 -o "${DEST_DIR}/${JAR}.tmp" "${URL}"
mv "${DEST_DIR}/${JAR}.tmp" "${DEST_DIR}/${JAR}"
verify "${DEST_DIR}/${JAR}"
echo "OK: ${DEST_DIR}/${JAR}"