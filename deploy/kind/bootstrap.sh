#!/usr/bin/env bash
# 启动受控的本地 kind/k3d 交付控制面。
# 默认拒绝未准备的 GitOps 源；仅支持 kind/kind-small，prod-like 仅可渲染。
# 用法：bootstrap.sh [--check-source|--skip-apps] [--skip-argocd] [--skip-eso] [--skip-envoy]
# GITOPS_CHECKOUT：已审核的本地 Git 仓库（工作区未提交文件不会被使用）。
# GITOPS_ROOT_REVISION：发布到集群内 Git 服务的完整提交 SHA，包含八个子 Application。
# GITOPS_CONTENT_REVISION：八个子 Application 的目标提交，包含 chart/values/目录资源。
# Git 服务必须事先在 argocd 命名空间内，以只读 ClusterIP 提供两个固定提交，不开放主机端口。
# KUBECONFIG：显式指定 kind-up/k3d-up 生成的 kubeconfig；可选 BOOTSTRAP_CONTEXT 用作一致性断言。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

ARGOCD_VERSION="${ARGOCD_VERSION:-v2.13.3}"
ESO_VERSION="${ESO_VERSION:-v0.14.4}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"
ROOT_APP_ENV="${ROOT_APP_ENV:-kind}"
GITOPS_CHECKOUT="${GITOPS_CHECKOUT:-}"
GITOPS_ROOT_REVISION="${GITOPS_ROOT_REVISION:-}"
GITOPS_CONTENT_REVISION="${GITOPS_CONTENT_REVISION:-}"
export GITOPS_SOURCE_URL='http://ontoaxis-git.argocd.svc.cluster.local:8080/ontoaxis.git'

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf '错误：%s\n' "$*" >&2; exit 1; }

SKIP_ARGOCD=0; SKIP_ESO=0; SKIP_ENVOY=0; SKIP_APPS=0; CHECK_SOURCE=0
for arg in "$@"; do
  case "$arg" in
    --skip-argocd) SKIP_ARGOCD=1 ;;
    --skip-eso) SKIP_ESO=1 ;;
    --skip-envoy) SKIP_ENVOY=1 ;;
    --skip-apps) SKIP_APPS=1 ;;
    --check-source) CHECK_SOURCE=1 ;;
    -h|--help) sed -n '2,9s/^# //p' "$0"; exit 0 ;;
    *) die "未知参数：$arg" ;;
  esac
done
case "$ROOT_APP_ENV" in
  kind|kind-small) ;;
  *) die '本地启动仅支持 kind 或 kind-small；prod-like 仅渲染' ;;
esac
[[ "$WAIT_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || die 'WAIT_TIMEOUT 必须是正整数秒'
[ "$CHECK_SOURCE" -eq 0 ] || [ "$SKIP_APPS" -eq 0 ] || die '--check-source 不可与 --skip-apps 共用'

prepare_gitops() {
  [ -d "$GITOPS_CHECKOUT" ] || die 'GitOps 前置未满足：GITOPS_CHECKOUT 必须指向已审核的本地仓库'
  [[ "$GITOPS_ROOT_REVISION" =~ ^[a-f0-9]{40}$ && "$GITOPS_CONTENT_REVISION" =~ ^[a-f0-9]{40}$ ]] \
    || die 'GitOps 前置未满足：GITOPS_ROOT_REVISION 和 GITOPS_CONTENT_REVISION 必须是完整 Git SHA'
  command -v git >/dev/null 2>&1 || die '未安装 Git'
  python3 -c 'import yaml' >/dev/null 2>&1 || die '需要 Python 3 和 PyYAML'
  # 禁止 partial clone 在预检过程中自动获取任何公网对象。
  unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
  export GIT_NO_LAZY_FETCH=1 GIT_ALLOW_PROTOCOL=file GIT_NO_REPLACE_OBJECTS=1
  python3 - "$ROOT" "$GITOPS_CHECKOUT" "$GITOPS_ROOT_REVISION" "$GITOPS_CONTENT_REVISION" "$ROOT_APP_ENV" "$GITOPS_SOURCE_URL" <<'PY'
from pathlib import Path
import subprocess
import sys
import yaml

root, checkout, root_rev, content_rev, env, url = sys.argv[1:]
root = Path(root)
apps_dir = "deploy/argocd/apps"
apps = {
    "00-namespaces.yaml": ("namespaces", -10, "argocd", "deploy/argocd/manifests/namespaces", False),
    "10-secrets.yaml": ("secrets", -5, "oa-system", "deploy/argocd/secrets/kind", False),
    "20-platform.yaml": ("platform", 0, "oa-system", "deploy/helm/ontoaxis-platform", True),
    "30-data.yaml": ("data", 1, "oa-data", "deploy/helm/ontoaxis-data", True),
    "40-app.yaml": ("app", 2, "oa-app", "deploy/helm/ontoaxis-app", True),
    "50-edge.yaml": ("edge", 3, "oa-system", "deploy/edge/generated", False),
    "60-observability.yaml": ("observability", 4, "oa-observability", "deploy/helm/ontoaxis-observability", True),
    "70-jobs.yaml": ("jobs", 5, "oa-data", "deploy/helm/ontoaxis-jobs", True),
}

def git(*args):
    return subprocess.check_output(["git", "-C", checkout, *args], text=True, stderr=subprocess.DEVNULL)

def require(ok, message):
    if not ok:
        raise ValueError(message)

try:
    # 仅检查提交对象；不扫描/打包可能带有重复 Application 或密钥的当前工作区。
    for revision in (root_rev, content_rev):
        require(git("cat-file", "-t", revision).strip() == "commit", "指定 SHA 不是本地提交")
    entries = dict(line.split("\t", 1)[::-1] for line in git("ls-tree", f"{root_rev}:{apps_dir}").splitlines())
    require(set(entries) == set(apps), "根提交必须仅包含八个标准、非递归的子 Application")
    for filename, (name, wave, namespace, path, is_helm) in apps.items():
        require(entries[filename].startswith("100644 blob "), f"{filename} 不是普通 YAML 文件")
        child = yaml.safe_load(git("show", f"{root_rev}:{apps_dir}/{filename}"))
        metadata, spec = child["metadata"], child["spec"]
        require(child["kind"] == "Application" and metadata["name"] == f"ontoaxis-{name}"
                and metadata["namespace"] == "argocd" and spec["project"] == "ontoaxis", f"{filename} 的应用身份不符")
        require(str(metadata["annotations"]["argocd.argoproj.io/sync-wave"]) == str(wave), f"{filename} 的 sync wave 不符")
        require(spec["destination"] == {"namespace": namespace, "server": "https://kubernetes.default.svc"},
                f"{filename} 的目标不符")
        require(git("cat-file", "-t", f"{content_rev}:{path}").strip() == "tree", f"{filename} 的 Git 内容路径缺失")
        expected = {"repoURL": url, "targetRevision": content_rev}
        if is_helm:
            sources = spec.get("sources", [])
            require("source" not in spec and isinstance(sources, list) and len(sources) == 2
                    and sources[0] == dict(expected, ref="values"), f"{filename} 的 Helm 值源不符")
            require(sources[1] == dict(expected, path=path, helm={"valueFiles": [
                "$values/deploy/values/common.yaml", f"$values/deploy/values/{env}.yaml",
            ]}), f"{filename} 的 Helm chart、commit 或环境 values 不符")
        else:
            directory = {"recurse": name != "edge"}
            if name == "edge":
                directory["include"] = "envoy-httproute.yaml"
                require(git("cat-file", "-t", f"{content_rev}:{path}/envoy-httproute.yaml").strip() == "blob",
                        "Envoy 生成清单缺失")
            require("sources" not in spec and spec.get("source") == dict(expected, path=path, directory=directory),
                    f"{filename} 的本地 Git 源、commit 或目录不符")
        auto = spec.get("syncPolicy", {}).get("automated", {})
        require(auto.get("prune") is True and auto.get("selfHeal") is True, f"{filename} 未启用 prune/selfHeal")
    for path in ("deploy/values/common.yaml", f"deploy/values/{env}.yaml"):
        require(git("cat-file", "-t", f"{content_rev}:{path}").strip() == "blob", "环境 values 缺失")
    project = yaml.safe_load((root / "deploy/argocd/projects/ontoaxis.yaml").read_text())
    project["spec"]["sourceRepos"] = [url]
    application = yaml.safe_load((root / "deploy/argocd/root-app.yaml").read_text())
    application["metadata"]["labels"]["ontoaxis.io/environment"] = env
    application["spec"].pop("sources", None)
    application["spec"]["source"] = dict(repoURL=url, targetRevision=root_rev, path=apps_dir, directory={"recurse": False})
    yaml.safe_dump_all([project, application], sys.stdout, sort_keys=False)
except (OSError, subprocess.CalledProcessError, yaml.YAMLError, KeyError, TypeError, ValueError, AttributeError) as error:
    print(f"GitOps 前置未满足：{error}", file=sys.stderr)
    sys.exit(1)
PY
}

if [ "$SKIP_APPS" -eq 0 ]; then
  gitops_manifest="$(prepare_gitops)" || die '未应用 AppProject 或根 Application'
  log "本地 GitOps 源预检通过（环境 ${ROOT_APP_ENV}，根 ${GITOPS_ROOT_REVISION}）"
  if [ "$CHECK_SOURCE" -eq 1 ]; then exit 0; fi
fi

command -v kubectl >/dev/null 2>&1 || die '未安装 kubectl'
if [ -z "${KUBECONFIG:-}" ] || [ ! -f "$KUBECONFIG" ]; then
  die '必须显式设置本地集群的 KUBECONFIG 文件'
fi
context="$(kubectl config current-context)"
case "$context" in
  kind-*|k3d-*) ;;
  *) die '只允许 kind-* 或 k3d-* kubeconfig 上下文' ;;
esac
[ "${BOOTSTRAP_CONTEXT:-$context}" = "$context" ] || die 'BOOTSTRAP_CONTEXT 与 KUBECONFIG 当前上下文不符'
server="$(kubectl config view --minify -o 'jsonpath={.clusters[0].cluster.server}')"
[[ "$server" =~ ^https://(127\.0\.0\.1|localhost):[0-9]+$ ]] || die 'Kubernetes API 必须监听本机回环地址'
kubectl cluster-info --request-timeout=10s >/dev/null || die '本地 Kubernetes 集群不可达'
log "目标集群：$context"

if [ "$SKIP_APPS" -eq 0 ]; then
  # 预配置的 Git 服务必须是固定集群内端点，且不得配置 ExternalIP。
  service="$(kubectl -n argocd --request-timeout=10s get service ontoaxis-git -o json)" \
    || die '未准备集群内只读 Git 服务 ontoaxis-git/argocd'
  GIT_SERVICE_JSON="$service" python3 - <<'PY' || die 'Git 服务必须是 ClusterIP，且只开放 TCP 8080、无 ExternalIP'
import json, os, sys
spec = json.loads(os.environ["GIT_SERVICE_JSON"])["spec"]
ports = spec.get("ports", [])
sys.exit(not (spec.get("type") == "ClusterIP" and spec.get("clusterIP") not in (None, "", "None")
              and not spec.get("externalIPs") and len(ports) == 1
              and ports[0].get("port") == 8080 and ports[0].get("protocol", "TCP") == "TCP"))
PY
fi

for ns in argocd external-secrets oa-system oa-app oa-data oa-observability; do
  kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "命名空间已就绪：$ns"
done

if [ "$SKIP_ARGOCD" -eq 0 ]; then
  log "安装 ArgoCD ${ARGOCD_VERSION}"
  kubectl apply -n argocd --server-side --force-conflicts \
    -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"
fi
if [ "$SKIP_ARGOCD" -eq 0 ] || [ "$SKIP_APPS" -eq 0 ]; then
  kubectl wait --for=condition=Established --timeout="${WAIT_TIMEOUT}s" \
    crd/applications.argoproj.io crd/appprojects.argoproj.io
  kubectl -n argocd wait --for=condition=Available --timeout="${WAIT_TIMEOUT}s" \
    deployment/argocd-server deployment/argocd-repo-server
  kubectl -n argocd rollout status statefulset/argocd-application-controller --timeout="${WAIT_TIMEOUT}s"
fi

if [ "$SKIP_ESO" -eq 0 ]; then
  if command -v helm >/dev/null 2>&1; then
    log "安装 ESO ${ESO_VERSION}"
    helm repo add external-secrets https://charts.external-secrets.io --force-update
    helm repo update external-secrets
    helm upgrade --install external-secrets external-secrets/external-secrets \
      --kube-context "$context" --namespace external-secrets --create-namespace \
      --set installCRDs=true --version "${ESO_VERSION#v}" --wait --timeout "${WAIT_TIMEOUT}s"
  else
    log "安装 ESO ${ESO_VERSION} 静态清单"
    kubectl apply --server-side --force-conflicts \
      -f "https://github.com/external-secrets/external-secrets/releases/download/${ESO_VERSION}/bundle.yaml"
  fi
fi
if [ "$SKIP_ESO" -eq 0 ] || [ "$SKIP_APPS" -eq 0 ]; then
  kubectl wait --for=condition=Established --timeout="${WAIT_TIMEOUT}s" \
    crd/externalsecrets.external-secrets.io crd/clustersecretstores.external-secrets.io
  kubectl -n external-secrets wait --for=condition=Available --timeout="${WAIT_TIMEOUT}s" \
    deployment/external-secrets deployment/external-secrets-webhook deployment/external-secrets-cert-controller
fi

if [ "$SKIP_ENVOY" -eq 0 ]; then
  log '安装 Envoy Gateway'
  bash "$ROOT/deploy/edge/kind/install-envoy-gateway.sh"
elif [ "$SKIP_APPS" -eq 0 ]; then
  kubectl wait --for=condition=Accepted --timeout="${WAIT_TIMEOUT}s" gatewayclass/eg
fi

# 密钥脚本负责读取 git-ignored 的 .env.kind，启动脚本不重复 source。
BOOTSTRAP_SECRET_NAMESPACE=external-secrets bash "$ROOT/scripts/delivery-secrets-bootstrap.sh" --env kind

if [ "$SKIP_APPS" -eq 0 ]; then
  log '应用受控 AppProject 和根 Application（仅提交 SHA）'
  printf '%s\n' "$gitops_manifest" | kubectl apply -f -
  log '控制面已就绪；应用同步/健康须由 deploy/kind/verify.sh 验证'
else
  log '仅控制面启动；未应用 GitOps 资源，也未验证交付'
fi
