#!/usr/bin/env python3
"""Private helpers for the isolated object-search Compose E2E."""

import json
import ipaddress
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys

OWNER_LABEL = "io.ontoaxis.search-e2e.run"
PROJECT_LABEL = "com.docker.compose.project"
ENDPOINTS = {"keycloak": 8080, "ontoaxis-core": 8080, "meilisearch": 7700, "search-worker": 8081}
SERVICES = {*ENDPOINTS, "postgres", "openfga", "authz-init"}
A = "11111111-1111-4111-8111-111111111111"
B = "22222222-2222-4222-8222-222222222222"


def require(ok, message):
    if not ok:
        raise ValueError(message)


def load(path):
    return json.loads(Path(path).read_text())


def new_socket(port):
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.bind(("127.0.0.1", port))
        sock.listen(1)
    except OSError:
        sock.close()
        raise
    return sock


def reserve(values):
    sockets = []
    try:
        for value in values:
            require(re.fullmatch(r"[0-9]{1,5}", value) and int(value) <= 65535, "invalid local host port")
            try:
                sockets.append(new_socket(int(value)))
            except OSError as exc:
                raise ValueError(f"loopback TCP port {value} unavailable: {exc}") from exc
        return [sock.getsockname()[1] for sock in sockets]
    finally:
        for sock in sockets:
            sock.close()


def generate(tmp, project, owner, realm, raw, password, key, *ports):
    tmp = Path(tmp).resolve()
    model = load(raw)
    require(set(ENDPOINTS).issubset(model["services"]), "Compose search services are missing")
    require(len(ports) == len(ENDPOINTS), "missing E2E port")
    port_map = dict(zip(ENDPOINTS, reserve(ports)))
    issuer = f"http://127.0.0.1:{port_map['keycloak']}"
    rep = load(realm)
    # Tokens survive bounded readiness/rebuild probes in this disposable realm.
    rep["accessTokenLifespan"] = 1800
    client = next(c for c in rep["clients"] if c["clientId"] == "ontoaxis-web")
    for mapper in client["protocolMappers"]:
        attribute = {"tenant-id": "tenant_id", "org-ids": "org_ids"}.get(mapper["name"])
        if attribute:
            mapper["protocolMapper"] = "oidc-usermodel-attribute-mapper"
            mapper["config"].pop("claim.value")
            mapper["config"].update({"user.attribute": attribute, "jsonType.label": "String",
                                     "multivalued": "false" if attribute == "tenant_id" else "true"})
    rep["users"] = [
        {"username": user, "firstName": "E2E", "lastName": "Synthetic", "email": user + "@ontoaxis.invalid",
         "enabled": True, "emailVerified": True, "realmRoles": roles,
         "attributes": {"tenant_id": [tenant], "org_ids": [tenant]},
         "credentials": [{"type": "password", "value": password, "temporary": False}]}
        for user, tenant, roles in (("e2e-a", A, ["tenant-admin", "member"]),
                                    ("e2e-b", B, ["tenant-admin", "member"]), ("e2e-denied", A, []))
    ]
    realm_path = tmp / "realm.json"
    realm_path.write_text(json.dumps(rep))
    # The local Keycloak container runs as a different UID; this file only
    # contains temporary synthetic credentials and its host directory is private.
    realm_path.chmod(0o644)
    (tmp / "password").write_text(password)
    (tmp / "meili-header").write_text(f"Authorization: Bearer {key}\n")
    lines = ["services:"]
    for service in model["services"]:
        require(re.fullmatch(r"[a-zA-Z0-9_-]+", service), "unexpected Compose service name")
        lines.append(f"  {service}:")
        lines.append(f"    labels: {{ {OWNER_LABEL}: {json.dumps(owner)} }}")
        if service not in ENDPOINTS:
            lines.append("    ports: !override []")
        else:
            lines.extend(["    ports: !override", f"      - \"127.0.0.1:{port_map[service]}:{ENDPOINTS[service]}\""])
        env = {}
        if service == "keycloak":
            lines.extend(["    volumes: !override", f"      - {json.dumps(str(realm_path) + ':/opt/keycloak/data/import/realm.json:ro')}"])
            env.update(KC_HOSTNAME=issuer, KC_BOOTSTRAP_ADMIN_PASSWORD=password)
        if service == "ontoaxis-core":
            env.update(ONTOAXIS_OIDC_ISSUER=issuer + "/realms/ontoaxis", ONTOAXIS_SEARCH_ENABLED="true",
                       ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT="")
        if service in {"ontoaxis-core", "search-worker"}:
            env.update(ONTOAXIS_SEARCH_URL="http://meilisearch:7700", ONTOAXIS_SEARCH_API_KEY=key)
        if service == "meilisearch":
            env.update(MEILI_MASTER_KEY=key, MEILI_NO_ANALYTICS="true")
        if env:
            lines.append("    environment: " + json.dumps(env))
    for kind in ("volumes", "networks"):
        if not model.get(kind):
            continue
        lines.append(f"{kind}:")
        for name in model[kind]:
            require(re.fullmatch(r"[a-zA-Z0-9_-]+", name), "unexpected Compose resource name")
            lines.extend([f"  {name}:", f"    labels: {{ {OWNER_LABEL}: {json.dumps(owner)} }}"])
    (tmp / "isolation.yaml").write_text("\n".join(lines) + "\n")
    print(*port_map.values())


def check(model, project, owner, *ports):
    require(model["name"] == project, "Compose project does not match")
    require(SERVICES.issubset(model["services"]), "Compose search/authz services are missing")
    port_map = dict(zip(ENDPOINTS, ports))
    require(len(port_map) == len(ENDPOINTS), "missing E2E port")
    for name, service in model["services"].items():
        require(not any(service.get(k) for k in ("container_name", "network_mode", "privileged", "devices", "volumes_from", "pid", "ipc")),
                f"unsafe Compose namespace or resource override: {name}")
        require(service.get("labels", {}).get(OWNER_LABEL) == owner, f"missing run label: {name}")
        bindings = service.get("ports", [])
        if name not in ENDPOINTS:
            require(not bindings, f"unexpected host port: {name}")
        else:
            require(len(bindings) == 1, f"unexpected host port count: {name}")
            p = bindings[0]
            require(ipaddress.ip_address(p.get("host_ip", "0.0.0.0")).is_loopback and p.get("host_ip") == "127.0.0.1"
                    and str(p.get("published")) == port_map[name] and p.get("target") == ENDPOINTS[name]
                    and p.get("protocol", "tcp") == "tcp", f"invalid loopback port binding: {name}")
    require(model["services"]["ontoaxis-core"].get("environment", {}).get("ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT") == "",
            "core telemetry must not export outside this fixture")
    require(model["services"]["meilisearch"].get("environment", {}).get("MEILI_NO_ANALYTICS") == "true",
            "Meilisearch analytics must be disabled")
    for category in ("networks", "volumes"):
        for name, item in model.get(category, {}).items():
            require(item.get("name") == f"{project}_{name}" and not item.get("external")
                    and not item.get("driver_opts") and item.get("labels", {}).get(OWNER_LABEL) == owner,
                    f"unowned Compose resource: {category}/{name}")


def docker(*args):
    env = {key: os.environ[key] for key in ("HOME", "PATH") if key in os.environ}
    result = subprocess.run(["docker", *args], env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=25)
    if result.returncode:
        raise ValueError(f"docker {args[0]} {args[1]} failed (exit {result.returncode})")
    return result.stdout


def run_bounded(seconds, *argv):
    require(re.fullmatch(r"[0-9]{1,4}", seconds) and 1 <= int(seconds) <= 3600,
            "invalid Compose command timeout (1–3600 seconds)")
    # Reap Compose and its subprocesses before the launcher inventories and
    # removes any partial resources. Do not use the CLI's detached wait alone.
    child = subprocess.Popen(argv, start_new_session=True)

    def terminate():
        try:
            os.killpg(child.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pass
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        child.wait()

    def interrupt(signum, _frame):
        signal.signal(signum, signal.SIG_IGN)
        terminate()
        raise SystemExit(128 + signum)

    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, interrupt)
    try:
        return child.wait(timeout=int(seconds))
    except subprocess.TimeoutExpired:
        print(f"FAIL Compose command exceeded {seconds}s; stopping its process group", file=sys.stderr)
        terminate()
        return 124


def check_context():
    endpoint = json.loads(docker("context", "inspect"))[0]["Endpoints"]["docker"]["Host"]
    require(endpoint.startswith("unix:///"), "E2E requires a local Unix Docker context")
    require(not os.environ.get("DOCKER_HOST") and not os.environ.get("DOCKER_CONTEXT") and not os.environ.get("DOCKER_CONFIG"),
            "E2E does not accept a Docker context override")


def resources(project):
    for kind, list_args in (("container", ("--all", "--no-trunc")),
                            ("network", ("--no-trunc",)), ("volume", ())):
        fmt = "{{json .}}"
        owned = docker(kind, "ls", *list_args, "--filter", f"label={PROJECT_LABEL}={project}", "--format", fmt)
        rows = {}
        for line in owned.splitlines():
            row = json.loads(line)
            name = row.get("Names") or row.get("Name")
            rows[name if kind == "volume" else row.get("ID")] = name
        for line in docker(kind, "ls", *list_args, "--format", fmt).splitlines():
            row = json.loads(line)
            name = row.get("Names") or row.get("Name")
            if name and (name == project or name.startswith((project + "-", project + "_"))):
                rows[name if kind == "volume" else row.get("ID")] = name
        for ref, name in rows.items():
            require(ref and name, f"incomplete {kind} inventory for project {project}")
            yield kind, ref, name


def resource_labels(kind, ref):
    item = json.loads(docker(kind, "inspect", ref))[0]
    return (item["Config"] if kind == "container" else item).get("Labels") or {}


def preflight(project):
    for kind, _, name in resources(project):
        raise ValueError(f"existing {kind} in project namespace {project!r}: {name}; refusing to take ownership")


def cleanup(project, owner):
    failed = False
    # Inspect all kinds for actual labels (network ls does not expose them on
    # every Docker version). Containers and networks use immutable full IDs;
    # volumes are inspected immediately before unforced removal.
    for kind, ref, _ in list(resources(project)):
        try:
            current = resource_labels(kind, ref)
            if current.get(OWNER_LABEL) != owner or current.get(PROJECT_LABEL) != project:
                continue
            docker(kind, "rm", *(('--force',) if kind == "container" else ()), ref)
        except (ValueError, subprocess.TimeoutExpired) as exc:
            print(f"cleanup: {kind}/{ref}: {exc}", file=sys.stderr)
            failed = True
    return not failed


def service_id(project, owner, service):
    found = []
    for kind, ref, _ in resources(project):
        if kind != "container":
            continue
        labels = resource_labels(kind, ref)
        if (labels.get(OWNER_LABEL) == owner and labels.get(PROJECT_LABEL) == project
                and labels.get("com.docker.compose.service") == service
                and labels.get("com.docker.compose.oneoff", "false").lower() == "false"):
            found.append(ref)
    require(len(found) == 1, f"expected one owned container for {service}")
    print(found[0])


def rebuild(path):
    rows = []
    for line in Path(path).read_text().splitlines():
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(row, dict) and row.get("msg") == "search rebuild complete":
            rows.append(row)
    require(len(rows) == 1, "expected a single JSON rebuild summary")
    row = rows[0]
    require(row.get("verified") is True and type(row.get("documents")) is int and row["documents"] > 0
            and isinstance(row.get("digest"), str) and re.fullmatch(r"[0-9a-f]{64}", row["digest"])
            and isinstance(row.get("index_uid"), str) and row["index_uid"].startswith("objects_"),
            "rebuild summary did not report a positive count, verified=true, index and SHA-256 digest")
    print(json.dumps({k: row[k] for k in ("documents", "digest", "index_uid")}, sort_keys=True))


def main(argv):
    cmd, *args = argv
    if cmd == "generate":
        generate(*args)
    elif cmd == "check":
        model, *rest = args
        check(load(model), *rest)
    elif cmd == "ports":
        reserve(args)
    elif cmd == "context":
        check_context()
    elif cmd == "bounded":
        return run_bounded(*args)
    elif cmd == "preflight":
        preflight(*args)
    elif cmd == "cleanup":
        return 0 if cleanup(*args) else 1
    elif cmd == "service-id":
        service_id(*args)
    elif cmd == "rebuild":
        rebuild(*args)
    else:
        raise ValueError("unknown E2E helper command")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except (ValueError, KeyError, OSError, subprocess.TimeoutExpired) as exc:
        print(f"object-search E2E: {exc}", file=sys.stderr)
        sys.exit(2)
