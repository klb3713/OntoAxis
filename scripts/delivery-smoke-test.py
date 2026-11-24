#!/usr/bin/env python3
"""Offline regression checks for delivery smoke, IdP and evidence classification."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread
from types import SimpleNamespace
import unittest
from unittest.mock import patch


SCRIPTS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("delivery_smoke", SCRIPTS / "delivery-smoke.py")
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class Fixture(BaseHTTPRequestHandler):
    def do_GET(self):
        base = f"http://127.0.0.1:{self.server.server_port}"
        if self.path == "/.well-known/openid-configuration":
            self.reply(200, dict(issuer=base, authorization_endpoint=base + "/auth",
                                 token_endpoint=base + "/token", jwks_uri=base + "/keys",
                                 id_token_signing_alg_values_supported=["RS256"]))
        elif self.path == "/keys":
            self.reply(200, dict(keys=[dict(kid="fixture", kty="RSA")]))
        elif self.path == "/redirect":
            self.send_response(302)
            self.send_header("Location", base + "/api")
            self.end_headers()
        elif self.path == "/api":
            self.reply(200 if self.headers.get("Authorization") == "Bearer fixture-good" else 401, {})
        else:
            self.reply(404, {})

    def reply(self, status, doc):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(doc).encode())

    def log_message(self, *args):
        pass


class DeliveryTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
        cls.thread = Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.url = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def idp(self, token="", control="", mode="", path="/api", url=None):
        env = dict(os.environ, OIDC_TEST_TOKEN=token, OIDC_CONTROL_TOKEN=control,
                   IDP_NEGATIVE=mode, OIDC_READONLY_API_PATH=path)
        result = subprocess.run(["bash", str(SCRIPTS / "delivery-idp-check.sh"),
                                 "--issuer", self.url, "--core-url", url or self.url],
                                env=env, text=True, capture_output=True, timeout=15)
        self.assertNotIn("fixture-good", result.stdout + result.stderr)
        self.assertNotIn("fixture-bad", result.stdout + result.stderr)
        return result

    def test_bearer_no_redirect_or_plain_nonlocal_http(self):
        self.assertEqual(smoke.request(self.url + "/redirect", "fixture-good")[0], 302)
        with self.assertRaisesRegex(smoke.CheckFailed, "non-local HTTP"):
            smoke.request("http://invalid.example/api", "fixture-good")

    def test_discovery_only_is_explicit(self):
        result = self.idp()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("live-token API check not executed", result.stdout)

    def test_negative_requires_distinct_control(self):
        for token, control in [("", ""), ("fixture-bad", ""), ("fixture-good", "fixture-good")]:
            with self.subTest(token_present=bool(token), control_present=bool(control)):
                self.assertNotEqual(self.idp(token, control, "audience").returncode, 0)
        self.assertNotEqual(self.idp("fixture-bad", "fixture-good", "unknown").returncode, 0)

    def test_candidate_rejected_only_after_positive_control(self):
        result = self.idp("fixture-bad", "fixture-good", "bad_signature")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("verify fixture semantics separately", result.stdout)
        denied_control = self.idp("fixture-good", "fixture-bad", "expired")
        self.assertNotEqual(denied_control.returncode, 0)
        self.assertIn("positive control did not return HTTP 200", denied_control.stderr)
        dead_api = self.idp("fixture-bad", "fixture-good", "issuer", url="http://127.0.0.1:0")
        self.assertNotEqual(dead_api.returncode, 0)
        self.assertIn("(got 000)", dead_api.stderr)

    def test_full_profile_requires_authorized_read(self):
        checker = smoke.Smoke(SimpleNamespace(profile="full"))
        with patch.dict(os.environ, {"SMOKE_READONLY_API_PATH": "/api", "ONTOAXIS_SMOKE_TOKEN": ""}):
            checker.edge_paths(self.url)
        checks = {c["name"]: c for c in checker.checks}
        self.assertFalse(checks["read-only-api"]["ok"])
        self.assertIn("authorized read not checked", checks["read-only-api"]["detail"])

    def test_gitops_missing_apps_fails(self):
        checker = smoke.Smoke(SimpleNamespace())
        checker.context = "fixture"
        with patch.object(checker, "kubectl", return_value={"items": []}):
            with self.assertRaisesRegex(smoke.CheckFailed, "missing ArgoCD Application"):
                checker.gitops()

    def test_gitops_requires_exact_inventory_and_stable_health(self):
        checker = smoke.Smoke(SimpleNamespace())
        checker.context = "fixture"
        apps = [dict(metadata=dict(name=name), spec=dict(project="ontoaxis",
                     destination=dict(server="https://kubernetes.default.svc")),
                     status=dict(sync=dict(status="Synced"), health=dict(status="Healthy")))
                for name in sorted(smoke.APP_NAMES)]
        with patch.object(checker, "kubectl", return_value={"items": apps}):
            self.assertIn("9 ArgoCD Applications", checker.gitops())
            apps[0]["status"]["operationState"] = {"phase": "Running"}
            with self.assertRaisesRegex(smoke.CheckFailed, "not stably Synced/Healthy"):
                checker.gitops()
            del apps[0]["status"]["operationState"]
            apps.append(dict(metadata=dict(name="ontoaxis-extra"), spec=dict(project="ontoaxis")))
            with self.assertRaisesRegex(smoke.CheckFailed, "unexpected ArgoCD Application"):
                checker.gitops()

    def test_image_coverage_and_owner_chain(self):
        from copy import deepcopy

        digest = "sha256:" + "a" * 64
        repo = "localhost:5000/fixture"
        ref = f"{repo}@{digest}"
        entries = [dict(component=key, repository=repo, digest=digest, commit="fixture", chartVersion="0.1.0")
                   for key in ("ontoaxis-core", "ontoaxis-web")]
        workloads, children = [], []
        for label in ("ontoaxis-core", "web"):
            namespace = "oa-app"
            meta = dict(namespace=namespace, uid=label, generation=3, labels={"ontoaxis.io/component": label})
            pod_spec = dict(containers=[dict(name=label, image=ref)])
            workloads.append(dict(kind="Deployment", metadata=meta,
                                  spec=dict(replicas=1, template=dict(spec=pod_spec)),
                                  status=dict(observedGeneration=3, updatedReplicas=1, readyReplicas=1)))
            owner = [dict(controller=True, uid=label, kind="Deployment")]
            uid = label + "-rs"
            children.append(dict(kind="ReplicaSet", metadata=dict(namespace=namespace, uid=uid, ownerReferences=owner)))
            owner = [dict(controller=True, uid=uid, kind="ReplicaSet")]
            children.append(dict(kind="Pod", metadata=dict(namespace=namespace, ownerReferences=owner),
                                 spec=pod_spec, status=dict(phase="Running", conditions=[dict(type="Ready", status="True")],
                                 containerStatuses=[dict(name=label, imageID=ref, ready=True)])))
        checker = smoke.Smoke(SimpleNamespace(env="kind"))
        checker.context = "fixture"
        with tempfile.TemporaryDirectory() as tmp:
            manifest = Path(tmp) / "release.yaml"
            manifest.write_text(json.dumps(dict(environment="kind", created_at="fixture", operator="fixture",
                                                components=entries)))
            with patch.dict(os.environ, {"RELEASE_MANIFEST": str(manifest)}):
                with patch.object(checker, "kubectl", side_effect=[dict(items=workloads), dict(items=children)]):
                    self.assertIn("2 release images", checker.running_images())
                extra = deepcopy(workloads[0])
                extra["metadata"]["labels"]["ontoaxis.io/component"] = "omitted"
                with patch.object(checker, "kubectl", return_value=dict(items=workloads + [extra])):
                    with self.assertRaisesRegex(smoke.CheckFailed, "missing pins=omitted"):
                        checker.running_images()
                unrelated = deepcopy(children)
                unrelated[1]["metadata"]["ownerReferences"][0]["uid"] = "not-the-deployment-replicaset"
                with patch.object(checker, "kubectl", side_effect=[dict(items=workloads), dict(items=unrelated)]):
                    with self.assertRaisesRegex(smoke.CheckFailed, "active owned replica count"):
                        checker.running_images()
                # Offline/kind-load mode: local loads report a config/import imageID instead
                # of the registry manifest digest; pinned mode accepts the pinned spec.
                local = deepcopy(children)
                local[1]["status"]["containerStatuses"][0]["imageID"] = (
                    "docker.io/library/import-2026-01-01@sha256:" + "b" * 64)
                with patch.dict(os.environ, {"SMOKE_IMAGE_ID_MODE": "pinned"}):
                    with patch.object(checker, "kubectl", side_effect=[dict(items=workloads), dict(items=local)]):
                        self.assertIn("2 release images", checker.running_images())
                tagged = deepcopy(children)
                tagged[1]["spec"]["containers"][0]["image"] = repo + ":fixture-tag"
                with patch.dict(os.environ, {"SMOKE_IMAGE_ID_MODE": "pinned"}):
                    with patch.object(checker, "kubectl", side_effect=[dict(items=workloads), dict(items=tagged)]):
                        self.assertIn("2 release images", checker.running_images())
                with patch.dict(os.environ, {"SMOKE_IMAGE_ID_MODE": "pinned"}):
                    with patch.object(checker, "kubectl", side_effect=[dict(items=workloads), dict(items=local)]):
                        with patch.dict(os.environ, {"SMOKE_IMAGE_ID_MODE": "manifest"}):
                            with self.assertRaisesRegex(smoke.CheckFailed, "unready or mismatched"):
                                checker.running_images()

    def test_evidence_never_overwrites_or_promotes_edge_only(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, source = (Path(tmp) / n for n in ("report.md", "report.json"))
            command = ["bash", str(SCRIPTS / "delivery-evidence.sh"), "--smoke", str(source), "--out", str(out)]
            source.write_text(json.dumps(dict(environment="kind", profile="edge", passed=True,
                                              checks=[dict(name="route", ok=True)])))
            result = subprocess.run(command, text=True, capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            written = out.read_text()
            self.assertIn("full acceptance not established", written)
            self.assertNotEqual(subprocess.run(command, capture_output=True, timeout=10).returncode, 0)
            self.assertEqual(out.read_text(), written)


if __name__ == "__main__":
    unittest.main(verbosity=2)
