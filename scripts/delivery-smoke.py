"""Read-only delivery verifier. This program deliberately distinguishes probes from acceptance."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

ROOT = Path(__file__).resolve().parent.parent
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
APP_NAMES = {"ontoaxis-" + x for x in (
    "root", "namespaces", "secrets", "platform", "data", "app", "edge", "observability", "jobs",
)}
EDGE_CHECKS = ("edge-port-forward", "core-healthz", "core-readyz", "read-only-api", "web-root", "search-path")
ALIASES = {"ontoaxis-web": "web", "ontoaxis-search-worker": "ontoaxis-search"}
WORKLOAD_NAMESPACES = {"oa-app", "oa-data", "oa-system", "oa-observability"}


class CheckFailed(Exception):
    pass


def require(condition, message):
    if not condition:
        raise CheckFailed(message)


def is_digest(value):
    return isinstance(value, str) and DIGEST.fullmatch(value) and value != "sha256:" + "0" * 64


def utcnow():
    return datetime.now(timezone.utc)


def timestamp(value):
    if re.fullmatch(r"\d{8}T\d{6}Z", value):
        return datetime.strptime(value, "%Y%m%dT%H%M%SZ").replace(tzinfo=timezone.utc)
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(result.tzinfo is not None, "marker timestamp must include timezone")
    return result


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


DIRECT = build_opener(ProxyHandler({}), NoRedirect())
NORMAL = build_opener(NoRedirect())


def request(url, token=""):
    endpoint = urlsplit(url)
    require(endpoint.scheme in {"http", "https"} and endpoint.hostname and not endpoint.username
            and not endpoint.password and not endpoint.fragment, "invalid HTTP endpoint")
    loopback = endpoint.hostname in {"localhost", "127.0.0.1", "::1"}
    if token:
        require(endpoint.scheme == "https" or loopback, "refusing bearer token over non-local HTTP")
        require(re.fullmatch(r"[A-Za-z0-9._~+/-]+=*", token), "invalid bearer token format")
    try:
        headers = {"Authorization": "Bearer " + token} if token else {}
        with (DIRECT if loopback else NORMAL).open(Request(url, headers=headers), timeout=5) as response:
            return response.status, response.read(1024 * 1024)
    except HTTPError as exc:
        exc.close()
        return exc.code, b""
    except (URLError, TimeoutError, OSError):
        return 0, b""


class Smoke:
    def __init__(self, args):
        self.args = args
        self.checks = []
        self.digest = ""
        self.images = {}
        self.context = ""
        self.workloads = []

    def run(self, name, check):
        try:
            detail = check()
            self.record(name, True, detail)
        except CheckFailed as exc:
            self.record(name, False, str(exc))
        except (OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError, AttributeError):
            self.record(name, False, "could not read or validate check input")

    def record(self, name, ok, detail, status=None):
        self.checks.append(dict(name=name, ok=ok, result=status or ("passed" if ok else "failed"), detail=detail))

    def kubectl(self, *args):
        result = subprocess.run(["kubectl", "--context", self.context, "--request-timeout=8s", *args],
                                capture_output=True, text=True, timeout=12, check=False)
        require(result.returncode == 0, "kubectl query failed: " + " ".join(args[:2]))
        return json.loads(result.stdout)

    def select_context(self):
        self.context = self.args.context or os.environ.get("SMOKE_KUBE_CONTEXT", "")
        if not self.context and self.args.env == "kind":
            self.context = "kind-ontoaxis-kind"
        require(bool(self.context), "set SMOKE_KUBE_CONTEXT to the approved staging-like context")

    def edge(self):
        proc = None
        port = None
        with tempfile.TemporaryFile(mode="w+b") as output:
            try:
                target = os.environ.get("SMOKE_EDGE_SERVICE", "")
                if not target:
                    namespace = os.environ.get("SMOKE_EDGE_NS", "envoy-gateway-system")
                    data = self.kubectl("-n", namespace, "get", "svc", "-l",
                                        "gateway.envoyproxy.io/owning-gateway-name=oa-edge,"
                                        "gateway.envoyproxy.io/owning-gateway-namespace=oa-system", "-o", "json")
                    require(len(data["items"]) == 1, "expected one oa-edge data-plane Service; set SMOKE_EDGE_SERVICE")
                    target = namespace + "/" + data["items"][0]["metadata"]["name"]
                require(re.fullmatch(r"[a-z0-9][a-z0-9.-]*/[a-z0-9][a-z0-9.-]*", target), "invalid edge Service")
                namespace, service = target.split("/")
                remote = os.environ.get("SMOKE_EDGE_TARGET_PORT", "8080")
                local = os.environ.get("SMOKE_EDGE_PORT", "")
                require(remote.isdecimal() and 0 < int(remote) <= 65535 and
                        (not local or (local.isdecimal() and 0 < int(local) <= 65535)), "invalid edge port")
                proc = subprocess.Popen(["kubectl", "--context", self.context, "-n", namespace,
                                         "port-forward", "--address", "127.0.0.1", f"svc/{service}", f"{local}:{remote}"],
                                        stdout=output, stderr=subprocess.STDOUT)
                for _ in range(60):
                    output.seek(0)
                    match = re.search(rb"^Forwarding from 127\.0\.0\.1:(\d+) -> \d+", output.read(), re.M)
                    if match and proc.poll() is None:
                        port = int(match.group(1))
                        break
                    if proc.poll() is not None:
                        break
                    time.sleep(0.25)
                require(port is not None, "kubectl did not establish its own loopback port-forward")
                self.record("edge-port-forward", True, f"{target} on loopback port {port}")
                self.edge_paths(f"http://127.0.0.1:{port}")
            except (CheckFailed, OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError) as exc:
                self.record("edge-port-forward", False, str(exc) if isinstance(exc, CheckFailed) else "edge setup failed")
                for name in EDGE_CHECKS[1:]:
                    self.record(name, False, "no verified port-forward", "not passed")
            finally:
                if proc is not None and proc.poll() is None:
                    proc.terminate()
                    try:
                        proc.wait(timeout=3)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait()

    def edge_paths(self, base):
        def http(path, expected, token=""):
            require(path.startswith("/") and not path.startswith("//"), "probe path must be relative to edge")
            status, _ = request(base + path, token)
            require(status in expected, f"HTTP {status:03d}; want {'/'.join(map(str, expected))}")
            return f"HTTP {status}"

        for name, path in (("core-healthz", "/healthz"), ("core-readyz", "/readyz"), ("web-root", "/")):
            self.run(name, lambda path=path: http(path, (200,)))

        def read_only():
            path = os.environ.get("SMOKE_READONLY_API_PATH", "/api/v1/tenancy/organizations")
            http(path, (401,))
            token = os.environ.get("ONTOAXIS_SMOKE_TOKEN", "")
            if self.args.profile != "edge":
                require(token, "authorized read not checked: set ONTOAXIS_SMOKE_TOKEN (anonymous HTTP 401 verified)")
            if token:
                http(path, (200,), token)
                return "anonymous HTTP 401; authorized HTTP 200"
            return "anonymous HTTP 401; diagnostic profile only"

        self.run("read-only-api", read_only)
        expected = tuple(int(x) for x in os.environ.get("SMOKE_SEARCH_EXPECT", "404").split(",") if x.isdecimal())
        if self.args.profile != "edge":
            expected = tuple(x for x in expected if x in (200, 404))
        self.run("search-path", lambda: http(os.environ.get("SMOKE_SEARCH_PATH", "/search"), expected))

    def gitops(self):
        require(self.context, "no approved Kubernetes context")
        data = self.kubectl("-n", "argocd", "get", "applications", "-o", "json")
        project_apps = [app for app in data["items"] if app.get("spec", {}).get("project") == "ontoaxis"]
        apps = {a["metadata"]["name"]: a for a in project_apps}
        require(len(apps) == len(project_apps), "duplicate ArgoCD Application name")
        missing = APP_NAMES - set(apps)
        require(not missing, "missing ArgoCD Application(s): " + ", ".join(sorted(missing)))
        unexpected = set(apps) - APP_NAMES
        require(not unexpected, "unexpected ArgoCD Application(s): " + ", ".join(sorted(unexpected)))
        for name, app in sorted(apps.items()):
            status = app.get("status", {})
            phase = status.get("operationState", {}).get("phase")
            require(not app.get("operation") and phase in (None, "Succeeded") and
                    status.get("sync", {}).get("status") == "Synced" and
                    status.get("health", {}).get("status") == "Healthy", name + " is not stably Synced/Healthy")
            require(app["spec"].get("destination", {}).get("server") == "https://kubernetes.default.svc",
                    name + " is not targeting the verified cluster")
        return f"{len(apps)} ArgoCD Applications Synced/Healthy"

    def running_images(self):
        require(self.context, "no approved Kubernetes context")
        manifest = os.environ.get("RELEASE_MANIFEST", str(ROOT / "deploy/releases" / (self.args.env + ".yaml")))
        loaded = subprocess.run(["ruby", "-ryaml", "-rjson", "-e",
                                 "puts JSON.generate(YAML.safe_load(File.read(ARGV.fetch(0))))", manifest],
                                capture_output=True, text=True, check=False, timeout=8)
        require(loaded.returncode == 0, "release manifest is missing or not valid YAML")
        release = json.loads(loaded.stdout)
        require(release["environment"] == self.args.env and release.get("created_at") and release.get("operator"),
                "release manifest has wrong environment or missing metadata")
        entries = release["components"]
        names = [entry["component"] for entry in entries]
        labels = {ALIASES.get(name, name) for name in names}
        require(len(names) == len(labels) and {"ontoaxis-core", "ontoaxis-web"}.issubset(names),
                "release must have unique workload components, including core and web")
        managed = self.kubectl("get", "deployments,statefulsets", "-A", "-l", "app.kubernetes.io/part-of=ontoaxis",
                               "-o", "json")["items"]
        workloads = {}
        for item in managed:
            meta = item["metadata"]
            if meta["namespace"] not in WORKLOAD_NAMESPACES or meta.get("deletionTimestamp"):
                continue
            label = meta.get("labels", {}).get("ontoaxis.io/component")
            require(label and label not in workloads, "managed workload missing a unique component label")
            workloads[label] = item
        require(set(workloads) == labels,
                "release/workload coverage mismatch: missing pins=" + ",".join(sorted(set(workloads) - labels)) +
                "; absent workloads=" + ",".join(sorted(labels - set(workloads))))
        live = self.kubectl("get", "replicasets,pods", "-A", "-o", "json")["items"]
        replica_sets = [item for item in live if item["kind"] == "ReplicaSet"]
        pods = [item for item in live if item["kind"] == "Pod"]

        def owned_by(item, kind, uids):
            return any(ref.get("controller") is True and ref.get("kind") == kind and ref.get("uid") in uids
                       for ref in item["metadata"].get("ownerReferences", []))

        images = {}
        for entry in entries:
            component, repo, digest = (entry[k] for k in ("component", "repository", "digest"))
            require(re.fullmatch(r"[a-z0-9][a-z0-9-]*", component) and
                    re.fullmatch(r"[a-z0-9][a-z0-9.:-]*(/[a-z0-9][a-z0-9._-]*)*", repo) and is_digest(digest)
                    and entry.get("commit") not in (None, "", "REPLACE_ME") and "chartVersion" in entry,
                    "release contains incomplete image pin")
            ref = f"{repo}@{digest}"
            label = ALIASES.get(component, component)
            workload = workloads[label]
            meta, desired, state = workload["metadata"], workload["spec"], workload.get("status", {})
            count = desired.get("replicas", 1)
            require(count > 0 and state.get("observedGeneration", 0) >= meta["generation"] and
                    state.get("readyReplicas") == count and state.get("updatedReplicas") == count,
                    component + " workload has not converged")
            pod_spec = desired["template"]["spec"]
            require({c["name"]: c["image"] for c in pod_spec["containers"]} == {label: ref}
                    and not pod_spec.get("initContainers"), component + " workload contains an unpinned image")
            kind = workload["kind"]
            owners = {meta["uid"]}
            if kind == "Deployment":
                owners = {rs["metadata"]["uid"] for rs in replica_sets if rs["metadata"]["namespace"] == meta["namespace"]
                          and owned_by(rs, "Deployment", owners)}
                kind = "ReplicaSet"
            replicas = [p for p in pods if p["metadata"]["namespace"] == meta["namespace"]
                        and owned_by(p, kind, owners) and not p["metadata"].get("deletionTimestamp")
                        and p.get("status", {}).get("phase") not in ("Succeeded", "Failed")]
            require(len(replicas) == count, component + " active owned replica count differs from its workload")
            for pod in replicas:
                containers = {c["name"]: c["image"] for c in pod["spec"]["containers"]}
                statuses = {s["name"]: s for s in pod["status"].get("containerStatuses", [])}
                status = statuses.get(label, {})
                image_id = re.sub(r"^[a-z-]+://", "", status.get("imageID", ""))
                if os.environ.get("SMOKE_IMAGE_ID_MODE", "manifest").strip().lower() == "pinned":
                    # Offline/kind-load mode: images are loaded into the node image store
                    # (kind load / ctr import), so imageID is a config digest or import-*
                    # reference rather than the registry manifest digest. Accept either the
                    # exact digest reference or a non-latest tag in the same repository,
                    # while still requiring a ready container and a non-empty imageID.
                    repo = ref.rsplit("@", 1)[0]
                    spec_image = containers.get(label, "")
                    same_repo_tag = (spec_image.startswith(repo + ":") and "@" not in spec_image
                                     and not spec_image.endswith(":latest"))
                    require((containers == {label: ref} or same_repo_tag) and not pod["spec"].get("initContainers")
                            and bool(image_id) and status.get("ready") is True and condition(pod, "Ready"),
                            component + " has an unready or mismatched running replica")
                else:
                    require(containers == {label: ref} and not pod["spec"].get("initContainers")
                            and image_id in (ref, digest) and status.get("ready") is True and condition(pod, "Ready"),
                            component + " has an unready or mismatched running replica")
            images[component] = ref
        self.images = images
        self.digest = images["ontoaxis-core"].split("@", 1)[1]
        return f"{len(images)} release images match all active replicas"

    def workload_signals(self):
        core = self.kubectl("-n", "oa-app", "get", "deployment", "ontoaxis-core", "-o", "json")
        status = core["status"]
        require(status.get("observedGeneration", 0) >= core["metadata"]["generation"] and condition(core, "Available"),
                "core Deployment not Available at current generation")
        container = next((c for c in core["spec"]["template"]["spec"]["containers"] if c["name"] == "ontoaxis-core"), {})
        require(container.get("livenessProbe", {}).get("httpGet", {}).get("path") == "/healthz" and
                container.get("readinessProbe", {}).get("httpGet", {}).get("path") == "/readyz", "core probes missing or incorrect")
        labels = core["spec"]["template"]["metadata"]["labels"]
        pdb = self.kubectl("-n", "oa-app", "get", "pdb", "ontoaxis-core-pdb", "-o", "json")
        selector = pdb.get("spec", {}).get("selector", {}).get("matchLabels", {})
        status = pdb["status"]
        require(selector and all(labels.get(k) == v for k, v in selector.items()) and
                status.get("observedGeneration", 0) >= pdb["metadata"]["generation"] and
                status.get("currentHealthy", 0) >= status.get("desiredHealthy", 0) > 0,
                "core PDB not effective for the workload")
        references = {v["name"]: v.get("valueFrom", {}).get("secretKeyRef", {}) for v in container.get("env", [])}
        secret = references.get("ONTOAXIS_DATABASE_URL", {})
        require(secret.get("name") and secret.get("key") and not secret.get("optional"), "core DB Secret reference missing")
        external = self.kubectl("-n", "oa-app", "get", "externalsecret", "-o", "json")
        require(any(x["spec"].get("target", {}).get("name") == secret["name"] and condition(x, "Ready")
                    and any(y.get("secretKey") == secret["key"] for y in x["spec"].get("data", []))
                    for x in external["items"]), "no Ready ExternalSecret mapping the required DB key")
        return "core probes, matching PDB and required ExternalSecret Ready (no Secret values read)"

    def idp(self):
        issuer = os.environ.get("OIDC_ISSUER", "")
        require(issuer and not issuer.endswith("/"), "set OIDC_ISSUER to the configured issuer, without trailing slash")
        endpoint = urlsplit(issuer)
        require(not endpoint.query and (endpoint.scheme == "https" or
                (self.args.env == "kind" and endpoint.hostname in ("127.0.0.1", "localhost", "::1"))),
                "IdP issuer must be HTTPS, or HTTP loopback for kind")
        deployment = self.kubectl("-n", "oa-app", "get", "deployment", "ontoaxis-core", "-o", "json")
        containers = deployment["spec"]["template"]["spec"]["containers"]
        require(any(c["name"] == "ontoaxis-core" and any(v.get("name") == "ONTOAXIS_OIDC_ISSUER" and
                    v.get("value") == issuer for v in c.get("env", [])) for c in containers),
                "OIDC_ISSUER does not match core configuration")
        code, body = request(issuer + "/.well-known/openid-configuration")
        require(code == 200, f"IdP discovery returned HTTP {code:03d}")
        doc = json.loads(body)
        require(doc.get("issuer") == issuer and "RS256" in doc.get("id_token_signing_alg_values_supported", [])
                and all(isinstance(doc.get(k), str) and urlsplit(doc[k]).scheme == endpoint.scheme
                        and urlsplit(doc[k]).netloc == endpoint.netloc
                        for k in ("jwks_uri", "token_endpoint", "authorization_endpoint")),
                "IdP discovery issuer, signing algorithm, or endpoints do not match configured issuer")
        code, body = request(doc["jwks_uri"])
        require(code == 200 and any(k.get("kid") and k.get("kty") == "RSA" for k in json.loads(body)["keys"]),
                "configured IdP JWKS has no usable RSA kid")
        return "configured issuer and JWKS verified; does not assert external IdP acceptance"

    def backup(self):
        directory = os.environ.get("SMOKE_BACKUP_DIR", "")
        require(directory, "set SMOKE_BACKUP_DIR to the exported backup directory")
        manifest = json.loads((Path(directory) / "manifest.json").read_text())
        require(manifest.get("environment") in (None, self.args.env) and manifest.get("backup_id") and
                manifest.get("pg_version") and int(manifest["schema_version"]) > 0, "backup manifest metadata invalid")
        require(timedelta(minutes=-5) <= utcnow() - timestamp(manifest["created_at"]) <= timedelta(hours=24),
                "backup not within the 24h evidence window")
        dump = Path(directory) / "ontoaxis.dump"
        require(dump.is_file() and dump.stat().st_size == int(manifest["size_bytes"]) > 0, "backup dump missing or wrong size")
        digest = hashlib.sha256()
        with dump.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
        require(digest.hexdigest() == manifest.get("sha256"), "backup SHA-256 mismatch")
        cron = self.kubectl("-n", "oa-data", "get", "cronjob", "ontoaxis-backup", "-o", "json")
        spec = cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]
        image = next((c["image"] for c in spec["containers"] if c["name"] == "backup"), "")
        repo, _, digest = image.partition("@")
        require(repo and is_digest(digest) and manifest.get("image_digest") == image,
                "backup image pin does not match the running release's CronJob template")
        return "recent backup manifest, dump size, SHA-256 and image pin verified"

    def rollback(self):
        path = os.environ.get("SMOKE_ROLLBACK_MARKER", "")
        require(path, "set SMOKE_ROLLBACK_MARKER to the successful rehearsal JSON")
        mark = json.loads(Path(path).read_text())
        require(mark.get("environment") == self.args.env and mark.get("context") == self.context and
                mark.get("action") == "rollback" and mark.get("smoke_passed") is True and bool(self.digest) and mark.get("digest") == self.digest and
                re.fullmatch(r"[0-9a-f]{40}", mark.get("revision", "")) and
                type(mark.get("schema_version")) is int and mark["schema_version"] > 0 and
                timestamp(mark["rolled_back_at"]) <= utcnow() + timedelta(minutes=5),
                "rollback marker does not describe a successful rollback to this running digest")
        return "successful rollback marker matches current release digest and environment"


def condition(resource, name):
    return any(c.get("type") == name and c.get("status") == "True" for c in resource.get("status", {}).get("conditions", []))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", choices=("kind", "staging-like", "prod-like"), default="kind")
    parser.add_argument("--profile", choices=("full", "readiness", "edge"), default="full")
    parser.add_argument("--context", help="approved Kubernetes context (overrides SMOKE_KUBE_CONTEXT)")
    parser.add_argument("--evidence-dir", type=Path, default=ROOT / "dist/delivery")
    args = parser.parse_args()
    require(args.env != "prod-like", "prod-like is render-only; live verification is disabled")
    started = utcnow()
    smoke = Smoke(args)
    try:
        smoke.select_context()
        smoke.edge()
    except CheckFailed as exc:
        smoke.record("edge-port-forward", False, str(exc))
        for name in EDGE_CHECKS[1:]:
            smoke.record(name, False, "no approved context", "not passed")
    if args.profile != "edge":
        for name, check in (("gitops", smoke.gitops), ("running-images", smoke.running_images),
                            ("probes-pdb-secret", smoke.workload_signals), ("idp-discovery", smoke.idp)):
            smoke.run(name, check)
    if args.profile == "full":
        smoke.run("backup-marker", smoke.backup)
        smoke.run("rollback-marker", smoke.rollback)
    passed = all(check["ok"] for check in smoke.checks)
    args.evidence_dir.mkdir(parents=True, exist_ok=True)
    report = dict(environment=args.env, env=args.env, profile=args.profile, context=smoke.context,
                  digest=smoke.digest, images=smoke.images,
                  started_at=started.isoformat(), finished_at=utcnow().isoformat(), checks=smoke.checks,
                  overall="passed" if passed else "failed", passed=passed)
    output = json.dumps(report, indent=2) + "\n"
    name = f"smoke-{args.env}.json"
    # Non-acceptance smoke cannot overwrite the default full acceptance report.
    if args.evidence_dir.resolve() == (ROOT / "dist/delivery").resolve() and args.profile != "full":
        name = f"smoke-{args.env}-{args.profile}.json"
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=args.evidence_dir, delete=False) as stream:
        stream.write(output)
    Path(stream.name).replace(args.evidence_dir / name)
    print(output, end="")
    return 0 if passed else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (CheckFailed, OSError) as exc:
        sys.exit(f"error: {exc}")
