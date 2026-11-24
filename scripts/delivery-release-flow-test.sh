#!/usr/bin/env bash
# Offline: temporary Git repository plus deterministic registry, kubectl and ArgoCD fixtures.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
ROOT="$ROOT" python3 - <<'PY'
import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(os.environ["ROOT"])
CHECKS = "edge-port-forward core-healthz core-readyz read-only-api web-root search-path gitops running-images probes-pdb-secret idp-discovery".split()
COMMIT = "a" * 40
BASE = """# preserved global settings
global:
  environment: {env}
  defaultReplicas: 2
components:
  web:
    replicas: 2 # preserved component settings
    image:
      digest: sha256:{digest} # preserved digest comment
      repository: example.invalid/legacy-web
  untouched:
    replicas: 7
  ontoaxis-core:
    image: {{}} # initially empty
"""

with tempfile.TemporaryDirectory(prefix="ontoaxis-release-flow-test-") as temporary:
    repo = Path(temporary) / "repo"
    repo.mkdir()
    for sub in ("scripts", "deploy/values", "deploy/releases", "deploy/kind", "bin", "proof", "dist/delivery"):
        (repo / sub).mkdir(parents=True)
    for script in ("delivery-promote.sh", "delivery-rollback.sh", "delivery-release-flow.rb"):
        shutil.copy(ROOT / "scripts" / script, repo / "scripts" / script)
    git_tool = shutil.which("git")
    env = {**os.environ, "PATH": f"{repo / 'bin'}:{os.environ['PATH']}", "TEST_REPO": str(repo),
           "GIT_TOOL": git_tool, "GIT_TERMINAL_PROMPT": "0", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null"}
    for key in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"):
        env.pop(key, None)

    def write(path, text, mode=None):
        target = repo / path
        target.write_text(text)
        if mode is not None:
            target.chmod(mode)

    def dump(path, data):
        write(path, json.dumps(data, indent=2) + "\n")

    results = []

    def run(*command, ok=True, contains=None, **extra):
        cp = subprocess.run(command, cwd=repo, env=env | extra, text=True, capture_output=True, check=False, timeout=30)
        if len(command) > 1 and command[1] in ("scripts/delivery-promote.sh", "scripts/delivery-rollback.sh"):
            results.append((command[1], ok))
        actual = cp.stdout + cp.stderr
        assert (cp.returncode == 0) is ok, (command, cp.returncode, actual)
        if contains:
            assert contains in actual, (command, contains, actual)
        return cp.stdout.strip()

    def git(*args):
        return run(git_tool, *args)

    def logs():
        command_log = repo / "command.log"
        return [json.loads(line) for line in command_log.read_text().splitlines()] if command_log.exists() else []

    def manifest(version, label):
        body = json.dumps({"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
                           "config": {"digest": "sha256:" + "f" * 64}, "layers": [], "test_label": label, "test_version": version}, separators=(",", ":"))
        digest = hashlib.sha256(body.encode()).hexdigest()
        path = f"{label}-{version}.json"
        write("proof/" + path, body)
        return "sha256:" + digest, path

    proof = {}
    release_data = {}
    for version in (1, 2):
        for target_env in ("kind", "staging-like", "prod-like"):
            images = []
            for component in ("ontoaxis-core", "ontoaxis-web", "postgres"):
                label = component + ("-" + target_env if component == "ontoaxis-web" else "")
                digest, path = manifest(version, label)
                image_repo = f"{target_env}.example.invalid/ontoaxis/{component}"
                images.append(dict(component=component, repository=image_repo, digest=digest, commit=COMMIT, chartVersion="0.1.0"))
                proof[f"{image_repo}@{digest}"] = path
            release_data[version, target_env] = dict(environment=target_env, schema_version=1, created_at="2026-01-01T00:00:00Z", components=images)
    dump("proof/index.json", proof)

    def smoke(release, profile="full", context="fixture-context"):
        checks = CHECKS + (["backup-marker", "rollback-marker"] if profile == "full" else [])
        by_name = {i["component"]: f"{i['repository']}@{i['digest']}" for i in release["components"]}
        return dict(environment=release["environment"], context=context, digest=release["components"][0]["digest"],
                    images=by_name, overall="passed", passed=True, profile=profile,
                    checks=[dict(name=key, ok=True) for key in checks])

    for target_env in ("kind", "staging-like", "prod-like"):
        write(f"deploy/values/{target_env}.yaml", BASE.format(env=target_env, digest="b" * 64))
    dump("deploy/releases/kind.yaml", release_data[2, "kind"])
    dump("deploy/releases/staging-like.yaml", release_data[1, "staging-like"])
    dump("proof/target.json", release_data[2, "staging-like"])
    dump("proof/prod.json", release_data[2, "prod-like"])
    dump("proof/previous.json", release_data[1, "staging-like"])
    dump("proof/previous-smoke.json", smoke(release_data[1, "staging-like"], "readiness"))
    dump("proof/kind-previous.json", release_data[1, "kind"])
    dump("proof/kind-previous-smoke.json", smoke(release_data[1, "kind"], "readiness", "kind-fixture"))
    write("proof/kind.kubeconfig", "# synthetic kubeconfig; used only by fake kubectl\n")
    dump("dist/delivery/smoke-kind.json", smoke(release_data[2, "kind"]))
    write("tracked.txt", "original\n")
    write(".gitignore", "/dist/\n/command.log\n/sync-state.json\n/proof/\n/bin/\n")
    write("scripts/delivery-render-check.sh", '#!/usr/bin/env bash\n[[ "${TEST_RENDER_FAIL:-0}" != 1 ]]\n')
    write("scripts/delivery-smoke.sh", '''#!/usr/bin/env bash
exec test-cli smoke "$@"
''')
    for shell in ("bootstrap", "verify"):
        write(f"deploy/kind/{shell}.sh", f'#!/usr/bin/env bash\nexec test-cli {shell} "$@"\n')
    write("bin/test-cli", r'''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import subprocess
import sys

args = sys.argv[1:]
cli = Path(sys.argv[0]).name
if cli == "test-cli":
    cli = args.pop(0)
with Path("command.log").open("a") as log:
    print(json.dumps([cli] + args), file=log)

def flag(name):
    return args[args.index(name) + 1]

def quit_if(name, default="0"):
    if os.getenv(name, default) == "1":
        raise SystemExit(5)

def load_yaml(path):
    output = subprocess.check_output(["ruby", "-rpsych", "-rjson", "-e", "puts JSON.generate(Psych.safe_load(File.read(ARGV.fetch(0))))", path])
    return json.loads(output)

if cli == "git":
    if args[0] in ("push", "fetch", "clone"):
        raise SystemExit(95)
    if args[0] == "ls-remote":
        print(f"{os.environ.get('TEST_REMOTE_REVISION', '0'*40)}\t{args[-1]}")
    else:
        os.execv(os.environ["GIT_TOOL"], ["git", *args])
elif cli == "docker":
    quit_if("TEST_REGISTRY_FAIL")
    index = json.loads(Path("proof/index.json").read_text())
    sys.stdout.write((Path("proof") / index[args[-1]]).read_text())
elif cli == "helm":
    values = load_yaml(args[-1])
    chart = args[2].split("/")[-1]
    omit = os.getenv("TEST_OMIT_IMAGE")
    for label, component in values["components"].items():
        if label == omit or not component.get("image"):
            continue
        actual_chart = "ontoaxis-platform" if label == "postgres" else "ontoaxis-app"
        if chart != actual_chart:
            continue
        image = component["image"]
        ref = f"{image['repository']}@{image['digest']}"
        pod = {"metadata": {"labels": {"ontoaxis.io/component": label, "ontoaxis.io/environment": values["global"]["environment"]}},
               "spec": {"containers": [{"name": label, "image": ref}]}}
        print("---\n" + json.dumps({"kind": "Deployment", "spec": {"template": pod}}))
elif cli == "kubectl":
    if args == ["config", "current-context"]:
        print("kind-fixture")
        raise SystemExit(0)
    context = flag("--context")
    assert context in ("fixture-context", "kind-fixture")
    env = "kind" if context == "kind-fixture" else "staging-like"
    if "config" in args:
        assert env == "kind" and "--minify" in args
        print(os.getenv("TEST_KUBE_API", "https://127.0.0.1:6443"))
    elif "service" in args:
        assert env == "kind" and "ontoaxis-git" in args
        print(json.dumps({"spec": {"type": os.getenv("TEST_SERVICE_TYPE", "ClusterIP"), "clusterIP": "10.0.0.7",
                                   "ports": [{"port": 8080, "protocol": "TCP"}]}}))
    elif "deployment" in args:
        assert "postgres" in args
        index = json.loads(Path("proof/index.json").read_text())
        ref = next(pin for pin, filename in index.items() if pin.startswith(env + ".example.invalid/ontoaxis/postgres@") and filename == "postgres-1.json")
        if os.getenv("TEST_POSTGRES_MISMATCH") == "1":
            ref = "other.example.invalid/postgres@sha256:" + "e" * 64
        print(json.dumps({"spec": {"template": {"spec": {"containers": [{"name": "postgres", "image": ref}]}}}}))
    elif "exec" in args:
        assert "deployment/postgres" in args and "default_transaction_read_only=on" in args[-1]
        assert "FROM public.goose_db_version ORDER BY version_id, id DESC" in args[-1] and "WHERE is_applied" in args[-1]
        quit_if("TEST_SCHEMA_FAIL")
        print(os.getenv("TEST_SCHEMA", "1"))
    else:
        synced = json.loads(Path("sync-state.json").read_text()) if Path("sync-state.json").exists() else {}
        apps = []
        git_url = "http://ontoaxis-git.argocd.svc.cluster.local:8080/ontoaxis.git" if env == "kind" else "https://github.example.invalid/acme/ontoaxis.git"
        desired = os.environ.get("TEST_CONTENT_REVISION", "0"*40) if env == "kind" else "main"
        for chart in ("ontoaxis-platform", "ontoaxis-app"):
            revision = synced.get(chart, "0"*40)
            if os.getenv("TEST_WRONG_REV") == "1":
                revision = "0"*40
            sources = [{"repoURL": git_url, "targetRevision": desired, "ref": "values"},
                       {"repoURL": git_url, "targetRevision": desired, "path": f"deploy/helm/{chart}",
                        "helm": {"valueFiles": [f"$values/deploy/values/{env}.yaml"]}}]
            if os.getenv("TEST_WRONG_SOURCE") == "1":
                sources[0]["repoURL"] = "https://wrong.example.invalid/acme/ontoaxis.git"
            if os.getenv("TEST_EXTRA_VALUES") == "1":
                sources[1]["helm"]["valueFiles"].append("$values/deploy/values/override.yaml")
            apps.append({"metadata": {"name": chart, "labels": {"ontoaxis.io/environment": env}},
                         "spec": {"project": "ontoaxis", "destination": {"server": "https://kubernetes.default.svc"}, "sources": sources},
                         "status": {"sync": {"status": "Synced", "revisions": [revision, revision]}, "health": {"status": "Healthy"}}})
        if env == "kind":
            root = os.getenv("TEST_ROOT_REVISION", "0"*40)
            apps.append({"metadata": {"name": "ontoaxis-root", "labels": {"ontoaxis.io/environment": env}},
                         "spec": {"project": "ontoaxis", "destination": {"server": "https://kubernetes.default.svc"},
                                  "source": {"repoURL": git_url, "targetRevision": root, "path": "deploy/argocd/apps", "directory": {"recurse": False}}},
                         "status": {"sync": {"status": "Synced", "revision": root}, "health": {"status": "Healthy"}}})
        print(json.dumps({"items": apps}))
elif cli == "argocd":
    quit_if("TEST_SYNC_FAIL")
    assert flag("--kube-context") in ("fixture-context", "kind-fixture") and args.count("--revisions") == 2
    assert flag("--app-namespace") == "argocd" and "sync" in args
    app = args[args.index("sync") + 1]
    sync = json.loads(Path("sync-state.json").read_text()) if Path("sync-state.json").exists() else {}
    sync[app] = flag("--revisions")
    Path("sync-state.json").write_text(json.dumps(sync))
elif cli == "smoke":
    quit_if("TEST_SMOKE_FAIL")
    env = flag("--env")
    assert flag("--profile") == "readiness" and flag("--context") == ("kind-fixture" if env == "kind" else "fixture-context")
    assert os.environ["RELEASE_MANIFEST"] == str(Path(f"deploy/releases/{env}.yaml").resolve())
    if os.getenv("TEST_NO_REPORT") != "1":
        path = "proof/kind-previous-smoke.json" if env == "kind" else "proof/previous-smoke.json"
        fixture = json.loads(Path(path).read_text())
        if os.getenv("TEST_SMOKE_BAD_DIGEST") == "1":
            fixture["digest"] = "sha256:" + "c"*64
        (Path(flag("--evidence-dir")) / f"smoke-{env}.json").write_text(json.dumps(fixture))
elif cli in ("bootstrap", "verify"):
    assert os.environ["GITOPS_ROOT_REVISION"] == os.environ["TEST_EXPECT_ROOT"]
    assert os.environ["GITOPS_CONTENT_REVISION"] == os.environ["TEST_EXPECT_CONTENT"]
    assert os.environ["ROOT_APP_ENV"] == "kind"
    if cli == "bootstrap":
        assert args == ["--check-source"] and os.environ["GITOPS_CHECKOUT"] == str(Path.cwd())
        quit_if("TEST_ROOT_PREFLIGHT_FAIL")
    else:
        assert args == ["--wait-only"] and os.environ["VERIFY_CONTEXT"] == "kind-fixture"
        quit_if("TEST_ROOT_VERIFY_FAIL")
else:
    raise AssertionError(cli)
''', 0o755)
    for command in ("helm", "docker", "kubectl", "argocd", "git"):
        (repo / "bin" / command).symlink_to("test-cli")

    git("init", "-q")
    git("config", "user.name", "Fixture Operator")
    git("config", "user.email", "test@example.invalid")
    git("remote", "add", "origin", "https://github.example.invalid/acme/ontoaxis.git")
    git("add", ".")
    git("commit", "-qm", "fixture baseline")
    base = git("rev-parse", "HEAD")
    write("tracked.txt", "staged unrelated content\n")
    git("add", "tracked.txt")
    values_path = repo / "deploy/values/staging-like.yaml"
    base_values = values_path.read_text()
    promote = ("bash", "scripts/delivery-promote.sh", "--from", "kind", "--to", "staging-like", "--target-release", "proof/target.json", "--oci-proof", "proof/index.json")
    rollback = ("bash", "scripts/delivery-rollback.sh", "--env", "staging-like", "--release", "proof/previous.json", "--previous-smoke", "proof/previous-smoke.json")
    good_smoke = smoke(release_data[2, "kind"])
    for changed, expected in [({"context": "other", "environment": "prod-like"}, "环境"),
                              ({"passed": "true"}, "原生布尔"), ({"digest": ""}, "摘要"),
                              ({"profile": "readiness"}, "级别"), ({"checks": []}, "检查项"),
                              ({"images": {}}, "不完全匹配")]:
        dump("dist/delivery/smoke-kind.json", good_smoke | changed)
        run(*promote, "--commit", ok=False, contains=expected)
    dump("dist/delivery/smoke-kind.json", good_smoke)
    run(*promote, "--digest", "sha256:" + "d" * 64, ok=False, contains="指定核心摘要")
    proof_bad = dict(proof)
    proof_bad[next(iter(proof_bad))] = "missing"
    core_key = f"{release_data[2, 'staging-like']['components'][0]['repository']}@{release_data[2, 'staging-like']['components'][0]['digest']}"
    proof_bad[core_key] = "ontoaxis-core-1.json"
    dump("proof/bad-index.json", proof_bad)
    run(*promote[:-1], "proof/bad-index.json", "--commit", ok=False, contains="摘要不符")
    target_bad = copy.deepcopy(release_data[2, "staging-like"])
    target_bad["components"][1]["digest"] = release_data[2, "kind"]["components"][1]["digest"]
    dump("proof/target.json", target_bad)
    run(*promote, ok=False, contains="禁止跨环境复用")
    dump("proof/target.json", release_data[2, "staging-like"])
    run(*promote, TEST_OMIT_IMAGE="web", ok=False, contains="未进入")
    run(*promote, "--commit", TEST_RENDER_FAIL="1", ok=False, contains="渲染门禁失败")
    assert git("rev-parse", "HEAD") == base and values_path.read_text() == base_values
    assert git("diff", "--cached", "--name-only") == "tracked.txt"
    values_path.write_text(base_values + "# human's uncommitted work\n")
    run(*promote, "--commit", ok=False, contains="有用户改动")
    values_path.write_text(base_values)
    run(*promote, contains="没有修改发布文件")
    assert git("rev-parse", "HEAD") == base and values_path.read_text() == base_values
    run(*promote, "--commit", contains="未推送、未同步")
    promoted = git("rev-parse", "HEAD")
    assert promoted != base
    assert set(git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").splitlines()) == {"deploy/values/staging-like.yaml", "deploy/releases/staging-like.yaml"}
    assert git("diff", "--cached", "--name-only") == "tracked.txt"
    assert "# preserved digest comment" in values_path.read_text() and "replicas: 7" in values_path.read_text()
    run(*promote, "--commit", ok=False, contains="同一发布")
    prod = ("bash", "scripts/delivery-promote.sh", "--from", "kind", "--to", "prod-like", "--target-release", "proof/prod.json", "--oci-proof", "proof/index.json", "--commit")
    run(*prod, ok=False, contains="--confirm")
    run(*prod, "--confirm", TEST_RENDER_FAIL="1", ok=False, contains="渲染门禁失败")
    assert not (repo / "deploy/releases/prod-like.yaml").exists() and git("diff", "--cached", "--name-only") == "tracked.txt"
    run(*prod, "--confirm", contains="人工变更控制")
    assert (repo / "deploy/releases/prod-like.yaml").is_file()
    before_rollback = git("rev-parse", "HEAD")
    for unsupported in ("kind-small", "prod-like"):
        run("bash", "scripts/delivery-rollback.sh", "--env", unsupported, "--confirm", "--context", "fixture-context",
            "--sync", before_rollback, ok=False, contains="拒绝连接集群")
    assert not any(command[0] in ("kubectl", "argocd") for command in logs())

    run(*rollback, "--commit", ok=False, contains="--context")
    run(*rollback, "--commit", "--context", "fixture-context", TEST_POSTGRES_MISMATCH="1", ok=False, contains="替换 PostgreSQL 容器")
    run(*rollback, "--commit", "--context", "fixture-context", TEST_SCHEMA="2", ok=False, contains="恢复备份到独立实例")
    run(*rollback, "--commit", "--context", "fixture-context", TEST_SCHEMA_FAIL="1", ok=False, contains="Kubernetes 操作失败")
    run(*rollback, "--commit", "--context", "fixture-context", TEST_REGISTRY_FAIL="1", ok=False, contains="镜像仓库检查")
    previous = copy.deepcopy(release_data[1, "staging-like"])
    previous.pop("schema_version")
    dump("proof/previous.json", previous)
    run(*rollback, "--commit", "--context", "fixture-context", ok=False, contains="缺少有效 schema_version")
    dump("proof/previous.json", release_data[1, "staging-like"])
    assert git("rev-parse", "HEAD") == before_rollback
    assert list((repo / "dist/delivery/rollback-attempts").glob("*/failure.json"))
    assert not list((repo / "dist/delivery").glob("rollback-*.json"))
    run(*rollback, "--commit", "--context", "fixture-context", contains="人工审阅并发布")
    revision = git("rev-parse", "HEAD")
    assert revision != before_rollback and git("diff", "--cached", "--name-only") == "tracked.txt"
    assert not any(command[0] == "argocd" for command in logs())
    assert not list((repo / "dist/delivery").glob("rollback-*.json"))

    sync = ("bash", "scripts/delivery-rollback.sh", "--env", "staging-like", "--context", "fixture-context", "--timeout", "1", "--sync", revision)
    run(*sync, ok=False, contains="GitOps 分支未指向")
    run(*sync, TEST_REMOTE_REVISION=revision, TEST_WRONG_SOURCE="1", ok=False, contains="源与本地 origin")
    run(*sync, TEST_REMOTE_REVISION=revision, TEST_EXTRA_VALUES="1", ok=False, contains="受审核的目标环境 GitOps values")
    run(*sync, TEST_REMOTE_REVISION=revision, TEST_SCHEMA="2", ok=False, contains="拒绝跨 schema")
    assert not any(command[0] == "argocd" for command in logs())
    for extra, expected in (({"TEST_SYNC_FAIL": "1"}, "ArgoCD 操作失败"),
                            ({"TEST_WRONG_REV": "1"}, "Synced/Healthy"),
                            ({"TEST_SMOKE_FAIL": "1"}, "烟测命令失败"),
                            ({"TEST_NO_REPORT": "1"}, "文件不存在"),
                            ({"TEST_SMOKE_BAD_DIGEST": "1"}, "摘要与发布清单")):
        run(*sync, TEST_REMOTE_REVISION=revision, ok=False, contains=expected, **extra)
        assert not list((repo / "dist/delivery").glob("rollback-*.json"))
    run(*sync, TEST_REMOTE_REVISION=revision, contains="回滚验证通过")
    markers = list((repo / "dist/delivery").glob("rollback-*.json"))
    assert len(markers) == 1
    marker = json.loads(markers[0].read_text())
    assert marker["revision"] == revision and marker["smoke_passed"] is True and marker["schema_version"] == 1
    assert marker["digest"] == release_data[1, "staging-like"]["components"][0]["digest"]

    kind_options = {"KUBECONFIG": str(repo / "proof/kind.kubeconfig")}
    kind_rollback = ("bash", "scripts/delivery-rollback.sh", "--env", "kind", "--context", "kind-fixture", "--timeout", "1")
    kind_commit = (*kind_rollback, "--commit", "--previous", "proof/kind-previous.json", "--previous-smoke", "proof/kind-previous-smoke.json")
    run(*kind_commit, ok=False, contains="独立的 KUBECONFIG", KUBECONFIG="")
    run(*kind_commit, ok=False, contains="回环 Kubernetes API", TEST_KUBE_API="https://cluster.example.invalid:6443", **kind_options)
    run(*kind_commit, contains="人工审阅并发布", **kind_options)
    kind_revision = git("rev-parse", "HEAD")
    root_revision = "b" * 40
    kind_sync = (*kind_rollback, "--sync", kind_revision, "--gitops-root-revision", root_revision)
    kind_options.update(TEST_EXPECT_ROOT=root_revision, TEST_EXPECT_CONTENT=kind_revision)
    commands_before = len(logs())
    run(*kind_sync[:-2], ok=False, contains="--gitops-root-revision", **kind_options)
    run(*kind_sync, ok=False, contains="根与内容提交预检失败", TEST_ROOT_PREFLIGHT_FAIL="1", **kind_options)
    run(*kind_sync, ok=False, contains="只开放集群内 TCP 8080", TEST_SERVICE_TYPE="NodePort", **kind_options)
    run(*kind_sync, ok=False, contains="运行中的根 Application 未同步", **kind_options)
    kind_options["TEST_ROOT_REVISION"] = root_revision
    run(*kind_sync, ok=False, contains="运行源未指向", **kind_options)
    assert not any(entry[0] == "argocd" or entry[:2] == ["git", "ls-remote"] for entry in logs()[commands_before:])
    kind_options["TEST_CONTENT_REVISION"] = kind_revision
    run(*kind_sync, ok=False, contains="本地 ArgoCD 应用未全部", TEST_ROOT_VERIFY_FAIL="1", **kind_options)
    assert not list((repo / "dist/delivery").glob("rollback-kind-*.json"))
    run(*kind_sync, contains="回滚验证通过", **kind_options)
    kind_markers = list((repo / "dist/delivery").glob("rollback-kind-*.json"))
    assert len(kind_markers) == 1 and json.loads(kind_markers[0].read_text())["revision"] == kind_revision
    assert not any(entry[:2] == ["git", "ls-remote"] for entry in logs()[commands_before:])
    assert not any(command[:2] == ["git", "push"] for command in logs())
for label, tool in (("晋升", "scripts/delivery-promote.sh"), ("回滚", "scripts/delivery-rollback.sh")):
    selected = [passed for name, passed in results if name == tool]
    print(f"{label}：{len(selected)} 个用例，{selected.count(True)} 个预期成功，{selected.count(False)} 个预期拒绝")
print("发布晋升/回滚离线回归通过（临时 Git 仓库；未访问镜像仓库、集群或远端）")
PY
