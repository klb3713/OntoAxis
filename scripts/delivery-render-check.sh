#!/usr/bin/env bash
# 渲染五套 chart × 四个环境，校验确定性、镜像摘要和 Kubernetes schema。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/dist/helm-render"
REQUIRE_SCHEMA=0

fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }
for arg in "$@"; do
  case "$arg" in
    --require-schema) REQUIRE_SCHEMA=1 ;;
    -h|--help) echo "usage: $0 [--require-schema]"; exit 0 ;;
    *) fail "unknown argument: $arg" ;;
  esac
done
command -v helm >/dev/null 2>&1 || fail 'helm not installed; render was not executed'
python3 -c 'import yaml' >/dev/null 2>&1 || fail 'Python 3 with PyYAML is required'
if command -v kubeconform >/dev/null 2>&1; then
  schema=1
elif [ "$REQUIRE_SCHEMA" -eq 1 ]; then
  fail 'kubeconform not installed; schema validation is required'
else
  schema=0
  echo 'NOT EXECUTED kubeconform: not installed (use --require-schema for the CI gate)'
fi

charts=(ontoaxis-platform ontoaxis-data ontoaxis-app ontoaxis-observability ontoaxis-jobs)
envs=(kind kind-small staging-like prod-like)
tmp="$(mktemp -d)"
cleanup() { rm -rf -- "${tmp:?}"; }
trap cleanup EXIT
mkdir -p "$tmp/rendered"

for chart in "${charts[@]}"; do
  for env in "${envs[@]}"; do
    label="$chart x $env"
    chart_dir="$ROOT/deploy/helm/$chart"
    overlay=(-f "$ROOT/deploy/values/common.yaml" -f "$ROOT/deploy/values/$env.yaml")
    manifest="$tmp/rendered/${chart}-${env}.yaml"
    helm lint "$chart_dir" "${overlay[@]}" || fail "helm lint: $label"
    helm template "oa-${chart#ontoaxis-}" "$chart_dir" "${overlay[@]}" >"$manifest" || fail "helm template first render: $label"
    helm template "oa-${chart#ontoaxis-}" "$chart_dir" "${overlay[@]}" >"$tmp/again.yaml" || fail "helm template second render: $label"
    python3 - "$manifest" "$tmp/again.yaml" <<'PY' || fail "deterministic render: $label"
import hashlib
import pathlib
import sys

hashes = [hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest() for path in sys.argv[1:]]
if hashes[0] != hashes[1]:
    print(f"render digests differ: {hashes[0]} != {hashes[1]}", file=sys.stderr)
    sys.exit(1)
print(f"sha256:{hashes[0]}")
PY
    printf 'PASS deterministic render %s\n' "$label"
  done
done

python3 - "$ROOT" "$tmp/rendered" <<'PY' || fail 'YAML or digest contract'
from pathlib import Path
import re
import sys
import yaml

root, rendered = map(Path, sys.argv[1:])
digest = re.compile(r"sha256:[a-f0-9]{64}\Z")
reference = re.compile(r"[^\s@]+@sha256:[a-f0-9]{64}\Z")

def untagged(repository):
    # 允许注册表地址带端口，但最后一个分量是仓库名，不能有 tag。
    return isinstance(repository, str) and re.fullmatch(r"[^\s@]+", repository) and ":" not in repository.rsplit("/", 1)[-1]

def walk(node, path, field, manifest):
    if isinstance(node, list):
        for index, value in enumerate(node):
            walk(value, path, f"{field}[{index}]", manifest)
    elif isinstance(node, dict):
        for key, value in node.items():
            location = f"{field}.{key}"
            if key == "image":
                if manifest and (not isinstance(value, str) or not reference.fullmatch(value) or not untagged(value.split("@", 1)[0])):
                    raise ValueError(f"{path}: {location} must be repository@sha256:<64 hex>")
                if not manifest:
                    if not isinstance(value, dict):
                        raise ValueError(f"{path}: {location} must be a repository/digest mapping")
                    if "repository" in value and not untagged(value["repository"]):
                        raise ValueError(f"{path}: {location}.repository must not include a tag/digest")
                    if "digest" in value and (not isinstance(value["digest"], str) or not digest.fullmatch(value["digest"])):
                        raise ValueError(f"{path}: {location}.digest must be sha256:<64 hex>")
                    if "tag" in value:
                        raise ValueError(f"{path}: {location}.tag is not allowed")
            walk(value, path, location, manifest)

try:
    source = sorted((root / "deploy/values").glob("*.yaml"))
    source += sorted((root / "deploy/helm").glob("*/values.yaml"))
    manifests = sorted(rendered.glob("*.yaml"))
    if len(manifests) != 20:
        raise ValueError(f"expected 20 renders, got {len(manifests)}")
    for path in source + manifests:
        with path.open(encoding="utf-8") as stream:
            for document in yaml.safe_load_all(stream):
                walk(document, path, "$", path.parent == rendered)
except (OSError, yaml.YAMLError, ValueError) as error:
    print(error, file=sys.stderr)
    sys.exit(1)
print(f"PASS YAML and digest contract ({len(manifests)} renders)")
PY

python3 - "$ROOT" "$tmp/rendered" <<'PY' || fail 'kind Secret ownership and key coverage'
from pathlib import Path
import sys
import yaml

root, rendered = map(Path, sys.argv[1:])

def documents(path):
    return [x for x in yaml.safe_load_all(path.read_text()) if x]

def require(ok, error):
    if not ok:
        raise ValueError(error)

wave = root / "deploy/argocd/secrets/kind"
wave_docs = [d for path in sorted(wave.glob("*.yaml")) for d in documents(path)]
for profile in ("kind", "kind-small"):
    rendered_docs = [d for path in rendered.glob(f"*-{profile}.yaml") for d in documents(path)]
    store_names = {d["metadata"]["name"] for d in wave_docs if d["kind"] == "ClusterSecretStore"}
    bootstrap = (root / "scripts/delivery-secrets-bootstrap.sh").read_text()
    targets = {}
    for d in wave_docs + rendered_docs:
        if d["kind"] != "ExternalSecret":
            continue
        spec = d["spec"]
        target = (d["metadata"]["namespace"], spec["target"]["name"])
        require(target not in targets, f"{profile}: duplicate Secret owner for {target}")
        require(d in wave_docs and spec["target"].get("creationPolicy") == "Owner", f"{profile}: unexpected chart Secret owner for {target}")
        require(spec["secretStoreRef"]["name"] in store_names, f"{profile}: unknown SecretStore for {target}")
        keys = set()
        for item in spec["data"]:
            key = item["secretKey"]
            remote = item["remoteRef"]
            require(key not in keys and remote["key"] == "ontoaxis-bootstrap" and
                    f'"{remote["property"]}:' in bootstrap, f"{profile}: invalid mapping for {target}/{key}")
            keys.add(key)
        targets[target] = keys
    for d in rendered_docs:
        if d["kind"] not in {"Deployment", "StatefulSet", "CronJob", "Job"}:
            continue
        namespace = d["metadata"]["namespace"]
        spec = d["spec"]
        if d["kind"] == "CronJob":
            spec = spec["jobTemplate"]["spec"]
        pod = spec["template"]["spec"]
        for c in pod.get("containers", []) + pod.get("initContainers", []):
            for env in c.get("env", []):
                ref = env.get("valueFrom", {}).get("secretKeyRef")
                if ref and not ref.get("optional"):
                    require(ref["key"] in targets.get((namespace, ref["name"]), set()),
                            f"{profile}: {namespace}/{ref['name']} lacks {ref['key']} for {c['name']}")
    print(f"PASS {profile} unique wave -5 Secret owners and workload key coverage ({len(targets)} targets)")
PY

if [ "$schema" -eq 1 ]; then
  kubeconform -ignore-missing-schemas -summary -strict "$tmp/rendered" || fail 'kubeconform schema validation'
fi

# 只发布本次验证通过的矩阵。固定文件名，下次运行原位覆盖。
mkdir -p "$OUT"
for chart in "${charts[@]}"; do
  for env in "${envs[@]}"; do
    file="${chart}-${env}.yaml"
    cp "$tmp/rendered/$file" "$OUT/$file"
  done
done
printf 'RESULT: PASS (schema validation: %s); manifests: %s\n' "$(if [ "$schema" -eq 1 ]; then echo passed; else echo not-executed; fi)" "$OUT"
