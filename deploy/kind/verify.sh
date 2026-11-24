#!/usr/bin/env bash
# 等待受控根应用与八个子应用的指定提交全部 Synced/Healthy，再运行烟测。
# 环境：KUBECONFIG、VERIFY_CONTEXT、VERIFY_TIMEOUT、VERIFY_SMOKE_PROFILE（默认 full）、
# GITOPS_ROOT_REVISION、GITOPS_CONTENT_REVISION、ROOT_APP_ENV（kind 或 kind-small）。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
wait_timeout="${VERIFY_TIMEOUT:-300}"
profile="${VERIFY_SMOKE_PROFILE:-full}"
root_rev="${GITOPS_ROOT_REVISION:-}"
content_rev="${GITOPS_CONTENT_REVISION:-}"
root_env="${ROOT_APP_ENV:-kind}"
evidence_dir="${VERIFY_EVIDENCE_DIR:-$ROOT/dist/delivery}"
trusted_context=0
wait_only=0
case "$#:${1:-}" in
  0:) ;;
  1:--wait-only) wait_only=1 ;;
  *) printf '用法：%s [--wait-only]\n' "$0" >&2; exit 2 ;;
esac

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf '验证失败：%s\n' "$*" >&2; exit 1; }
finish() {
  local rc=$?
  if [ "$rc" -ne 0 ] && [ "$trusted_context" -eq 1 ]; then
    log '收集本地交付诊断（保留原退出码）' >&2
    bash "$ROOT/scripts/delivery-diag.sh" || log '诊断脚本也失败' >&2
  fi
  exit "$rc"
}
trap finish EXIT

case "$profile" in full|readiness) ;; *) die 'VERIFY_SMOKE_PROFILE 必须是 full 或 readiness' ;; esac
case "$root_env" in kind|kind-small) ;; *) die 'ROOT_APP_ENV 必须是 kind 或 kind-small' ;; esac
[[ "$wait_timeout" =~ ^[1-9][0-9]*$ ]] || die 'VERIFY_TIMEOUT 必须是正整数秒'
[[ "$root_rev" =~ ^[a-f0-9]{40}$ && "$content_rev" =~ ^[a-f0-9]{40}$ ]] \
  || die '必须指定完整的 GITOPS_ROOT_REVISION 和 GITOPS_CONTENT_REVISION（不能从运行状态猜测）'
command -v kubectl >/dev/null 2>&1 || die '未安装 kubectl'
command -v python3 >/dev/null 2>&1 || die '未安装 Python 3'
if [ -z "${KUBECONFIG:-}" ] || [ ! -f "$KUBECONFIG" ]; then
  die '必须显式设置本地集群的 KUBECONFIG 文件'
fi
context="$(kubectl config current-context)"
case "$context" in kind-*|k3d-*) ;; *) die '只允许 kind-* 或 k3d-* kubeconfig 上下文' ;; esac
[ "${VERIFY_CONTEXT:-${BOOTSTRAP_CONTEXT:-$context}}" = "$context" ] || die 'VERIFY_CONTEXT 与 KUBECONFIG 当前上下文不符'
server="$(kubectl config view --minify -o 'jsonpath={.clusters[0].cluster.server}')"
[[ "$server" =~ ^https://(127\.0\.0\.1|localhost):[0-9]+$ ]] || die 'Kubernetes API 必须监听本机回环地址'
trusted_context=1
kubectl cluster-info --request-timeout=10s >/dev/null || die '本地 Kubernetes 集群不可达'

check_apps() {
  python3 - "$root_rev" "$content_rev" "$root_env" 3<<<"$1" <<'PY'
import json
import os
import sys

root_rev, content_rev, env = sys.argv[1:]
root_name = "ontoaxis-root"
local_url = "http://ontoaxis-git.argocd.svc.cluster.local:8080/ontoaxis.git"
expected_apps = {
    root_name: 1, "ontoaxis-namespaces": 1, "ontoaxis-secrets": 1,
    "ontoaxis-platform": 2, "ontoaxis-data": 2, "ontoaxis-app": 2,
    "ontoaxis-edge": 1, "ontoaxis-observability": 2, "ontoaxis-jobs": 2,
}
pending = []
try:
    with os.fdopen(3, encoding="utf-8") as stream:
        items = json.load(stream)["items"]
    apps = {item["metadata"]["name"]: item for item in items}
    if len(apps) != len(items):
        raise ValueError("ArgoCD 应用列表中存在重复名称")
    unknown = {name for name, item in apps.items() if item.get("spec", {}).get("project") == "ontoaxis"} - expected_apps.keys()
    if unknown:
        raise ValueError("ontoaxis 项目有未审核的 Application：" + ", ".join(sorted(unknown)))
    for name, num_sources in expected_apps.items():
        if name not in apps:
            pending.append(f"{name}=缺失")
            continue
        app = apps[name]
        spec = app["spec"]
        revision = root_rev if name == root_name else content_rev
        sources = spec.get("sources") or [spec.get("source", {})]
        if (spec.get("project") != "ontoaxis" or len(sources) != num_sources
                or any(s.get("repoURL") != local_url or s.get("targetRevision") != revision for s in sources)):
            # 根在 bootstrap 中已指定，错误即失败；子应用可能尚待根更新，继续限时检查。
            if name == root_name:
                raise ValueError(f"{name} 的项目、本地 Git 源或目标提交不符")
            pending.append(f"{name}=源或目标 SHA 未收敛")
            continue
        if name == root_name and (app["metadata"].get("labels", {}).get("ontoaxis.io/environment") != env
                                  or sources[0].get("path") != "deploy/argocd/apps"
                                  or sources[0].get("directory", {}).get("recurse") is not False):
            raise ValueError(f"{name} 的部署环境或非递归目录配置不符")
        if name == "ontoaxis-edge" and (sources[0].get("path") != "deploy/edge/generated" or
                                        sources[0].get("directory") != {"recurse": False, "include": "envoy-httproute.yaml"}):
            raise ValueError("edge Application 只能读取 Envoy 生成清单，不能读取 Traefik 配置")
        if spec.get("destination", {}).get("server") != "https://kubernetes.default.svc":
            raise ValueError(f"{name} 必须部署至本集群")
        state = app.get("status", {})
        if state.get("operationState", {}).get("phase") == "Running":
            pending.append(f"{name}=同步操作仍在运行")
            continue
        sync = state.get("sync", {})
        status = sync.get("status", "Unknown")
        health = state.get("health", {}).get("status", "Unknown")
        synced_revisions = sync.get("revisions") if num_sources > 1 else [sync.get("revision")]
        if status != "Synced" or health != "Healthy" or synced_revisions != [revision] * num_sources:
            conditions = "/".join(c.get("type", "Unknown") for c in state.get("conditions", []))
            pending.append(f"{name}=同步:{status}/健康:{health}/修订:{'匹配' if synced_revisions == [revision] * num_sources else '未匹配'}"
                           + (f"/条件:{conditions}" if conditions else ""))
except (KeyError, TypeError, ValueError, AttributeError) as error:
    print(f"无效的 ArgoCD 应用状态：{error}", file=sys.stderr)
    sys.exit(2)
if pending:
    print("；".join(pending))
    sys.exit(1)
PY
}

log "等待 9 个 ArgoCD Application（${root_env}，超时 ${wait_timeout}s）"
deadline=$((SECONDS + wait_timeout))
while :; do
  if state="$(kubectl -n argocd --request-timeout=10s get applications.argoproj.io -o json 2>/dev/null)"; then
    if summary="$(check_apps "$state")"; then break; else app_rc=$?; fi
    [ "$app_rc" -eq 1 ] || die '根应用或 ArgoCD 状态配置校验失败'
  else
    summary='无法读取 ArgoCD Application（CRD、集群 API 或权限尚未就绪）'
  fi
  log "$summary"
  remaining=$((deadline - SECONDS))
  [ "$remaining" -gt 0 ] || die 'ArgoCD 同步/健康/提交校验超时，未运行烟测'
  if [ "$remaining" -gt 5 ]; then remaining=5; fi
  sleep "$remaining"
done

if [ "$wait_only" -eq 1 ]; then
  log '应用均 Synced/Healthy 且已同步至指定提交；仅等待，未执行烟测'
  exit 0
fi
log "应用均 Synced/Healthy 且已同步至指定提交；运行 ${profile} 烟测"
bash "$ROOT/scripts/delivery-smoke.sh" --env kind --context "$context" --profile "$profile" --evidence-dir "$evidence_dir" || {
  smoke_rc=$?
  printf '验证失败：%s 烟测退出码 %s\n' "$profile" "$smoke_rc" >&2
  exit "$smoke_rc"
}
log "验证通过（${profile} 范围）"
