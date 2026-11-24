#!/usr/bin/env python3
"""Offline regression tests for the object-search E2E launcher (no Docker daemon)."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("search_e2e", ROOT / "scripts/object-search-e2e-support.py")
support = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(support)

FAKE_DOCKER = r'''#!PYTHON
import json, os
from pathlib import Path
import sys
root = Path(__file__).resolve().parent
args = sys.argv[1:]
mode=(root / 'mode').read_text()
with (root / 'commands').open('a') as log:
    log.write(json.dumps(args)+'\n')
if args[:2] == ['context', 'inspect']:
    print(json.dumps([{'Endpoints': {'docker': {'Host':'unix:///tmp/fake-search-e2e'}}}]))
elif len(args) > 1 and args[1] == 'ls':
    list_kind=args[0]
    project=(root / 'project').read_text() if (root / 'project').exists() else 'ontoaxis-search-e2e-test'
    used=(root / 'started').exists()
    if used:
        owner=(root / 'owner').read_text()
        label='com.docker.compose.project='+project+',io.ontoaxis.search-e2e.run='+owner
        foreign='com.docker.compose.project='+project+',io.ontoaxis.search-e2e.run=other-owner'
        if list_kind == 'container':
            print(json.dumps({'Names':project+'-own', 'ID':'owned-id', 'Labels':label}))
            print(json.dumps({'Names':project+'-foreign', 'ID':'foreign-id', 'Labels':foreign}))
        if list_kind == 'network':
            print(json.dumps({'Name':project+'_default','ID':'owned-network-id','Labels':label}))
            print(json.dumps({'Name':project+'_other','ID':'foreign-network-id','Labels':foreign}))
        if list_kind == 'volume':
            print(json.dumps({'Name':project+'_data','Labels':label}))
            print(json.dumps({'Name':project+'_foreign','Labels':foreign}))
    elif mode == list_kind:
        # Empty Compose labels are intentional: name collisions also refuse.
        print(json.dumps({'Name':project+'_existing','ID':'foreign-id','Labels':''}))
    elif mode == 'inventory-error' and list_kind == 'network':
        sys.exit(9)
elif len(args) > 2 and args[1] == 'inspect':
    labels={'com.docker.compose.project':(root / 'project').read_text(),
            'io.ontoaxis.search-e2e.run':'other-owner' if 'foreign' in args[2] else (root / 'owner').read_text()}
    print(json.dumps([{'Config':{'Labels':labels}} if args[0]=='container' else {'Labels':labels}]))
elif len(args) > 1 and args[1] == 'rm':
    if 'foreign' in ' '.join(args) or (mode=='timeout' and not (root/'reaped').exists()):
        sys.exit(99)
elif args[0] == 'compose':
    project=args[args.index('-p')+1]
    (root / 'project').write_text(project)
    labels={}
    ep={'keycloak':8080,'ontoaxis-core':8080,'meilisearch':7700,'search-worker':8081}
    services={name:{} for name in [*ep,'postgres','trino','web','openfga','authz-init']}
    model={'name':project,'services':services,'volumes':{'data':{'name':project+'_data'}},
           'networks':{'default':{'name':project+'_default'}}}
    if 'config' in args:
        overlay=Path(args[args.index('config')-1])
        if overlay.name=='isolation.yaml':
            import re
            text=overlay.read_text()
            owner=re.search(r'io.ontoaxis.search-e2e.run: "([0-9a-f]+)"',text)[1]
            ports=re.findall(r'"127.0.0.1:(\d+):\d+"',text)
            labels={'io.ontoaxis.search-e2e.run':owner}
            (root / 'owner').write_text(owner)
            for name,d in services.items():
                d['labels']=labels
            services['ontoaxis-core']['environment']={'ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT':''}
            services['meilisearch']['environment']={'MEILI_NO_ANALYTICS':'true'}
            for name,port in zip(ep,ports):
                services[name]['ports']=[{'host_ip':'127.0.0.1','published':port,'target':ep[name]}]
            if mode=='public-binding':
                services['keycloak']['ports'][0]['host_ip']='0.0.0.0'
            for c in ('volumes','networks'):
                for d in model[c].values():
                    d['labels']=labels
        print(json.dumps(model))
    elif 'up' in args:
        (root / 'started').touch()
        if mode=='timeout':
            import signal, time
            def term(*_):
                (root/'reaped').touch()
                sys.exit(143)
            signal.signal(signal.SIGTERM,term)
            time.sleep(30)
        sys.exit(99)
    else:
        sys.exit(98)
else:
    sys.exit(97)
'''


class LauncherSafety(unittest.TestCase):
    def run_launcher(self, mode, project="ontoaxis-search-e2e-test", **options):
        env = {"PATH": f"{self.scratch}:{os.environ['PATH']}", "HOME": os.environ["HOME"], **options}
        if project is not None:
            env["E2E_PROJECT"] = project
        (self.scratch / "mode").write_text(mode)
        (self.scratch / "commands").write_text("")
        result = subprocess.run(["bash", str(ROOT / "scripts/object-search-e2e.sh")],
                                capture_output=True, text=True, env=env, timeout=12)
        calls = [json.loads(line) for line in (self.scratch / "commands").read_text().splitlines()]
        return result, calls

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.scratch = Path(self.temp.name)
        stub = self.scratch / "docker"
        stub.write_text(FAKE_DOCKER.replace("#!PYTHON", "#!" + sys.executable, 1))
        stub.chmod(0o700)

    def assert_no_mutations(self, calls):
        self.assertFalse(any({"up", "down", "rm", "pause", "unpause", "start", "stop"}.intersection(call) for call in calls), calls)

    def test_refuses_each_existing_resource_without_cleanup(self):
        for kind in ("container", "volume", "network"):
            with self.subTest(kind=kind):
                result, calls = self.run_launcher(kind)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f"existing {kind}", result.stderr)
                self.assert_no_mutations(calls)

    def test_inventory_error_fails_closed(self):
        result, calls = self.run_launcher("inventory-error")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("docker network ls failed", result.stderr)
        self.assert_no_mutations(calls)

    def test_process_real_loopback_port_collision_refuses_start(self):
        # The stub reports no Docker ports; the operating-system bind catches it.
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen(1)
            result, calls = self.run_launcher("port-check", E2E_CORE_PORT=str(listener.getsockname()[1]))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("loopback TCP port", result.stderr)
        self.assert_no_mutations(calls)

    def test_public_port_from_render_is_rejected(self):
        result, calls = self.run_launcher("public-binding")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid loopback", result.stderr)
        self.assert_no_mutations(calls)

    def test_old_url_overrides_and_remote_docker_rejected(self):
        for env in ({"E2E_CORE_URL": "http://example.invalid"}, {"DOCKER_HOST": "tcp://example.invalid:2375"}):
            with self.subTest(env=env):
                result, calls = self.run_launcher("reject", **env)
                self.assertNotEqual(result.returncode, 0)
                self.assert_no_mutations(calls)

    def test_partial_startup_cleans_only_labels_with_this_run(self):
        result, calls = self.run_launcher("start-fails")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("partial startup is cleaned by run labels", result.stderr)
        removals = [call for call in calls if len(call) > 1 and call[1] == "rm"]
        self.assertEqual(removals, [["container", "rm", "--force", "owned-id"],
                                    ["network", "rm", "owned-network-id"],
                                    ["volume", "rm", "ontoaxis-search-e2e-test_data"]])
        self.assertFalse(any("down" in call for call in calls))

    def test_startup_timeout_stops_compose_before_cleanup(self):
        result, calls = self.run_launcher("timeout", E2E_START_TIMEOUT="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("exceeded 1s", result.stderr)
        self.assertTrue((self.scratch / "reaped").exists())
        self.assertIn(["container", "rm", "--force", "owned-id"], calls)
        self.assertNotIn("cleanup incomplete", result.stderr)

    def test_default_projects_are_unique(self):
        statuses = []
        for _ in range(2):
            result, calls = self.run_launcher("public-binding", project=None)
            self.assertNotEqual(result.returncode, 0)
            self.assert_no_mutations(calls)
            statuses.append((self.scratch / "project").read_text())
        self.assertNotEqual(statuses[0], statuses[1])
        self.assertTrue(all(p.startswith("ontoaxis-search-e2e-") for p in statuses))


class SummaryContract(unittest.TestCase):
    def test_rebuild_parser_requires_verified_digest_and_ignores_other_logs(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp) / "worker.log"
            valid = {"msg":"search rebuild complete", "verified":True, "documents":1,
                     "digest":"a"*64, "index_uid":"objects_v1_fixture"}
            log.write_text("warning from Compose\n" + json.dumps(valid) + "\n")
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                support.rebuild(log)
            self.assertEqual(json.loads(out.getvalue()), {k:valid[k] for k in ("digest", "documents", "index_uid")})
            for bad in ({**valid, "digest":"missing"}, {**valid, "verified":False}, {**valid, "documents":0}):
                log.write_text(json.dumps(bad) + "\n")
                with self.assertRaises(ValueError):
                    support.rebuild(log)
            log.write_text((json.dumps(valid) + "\n") * 2)
            with self.assertRaises(ValueError):
                support.rebuild(log)

    def test_inventory_cleanup_rechecks_volume_labels_and_never_forces_it(self):
        labels = {support.OWNER_LABEL:"owned",support.PROJECT_LABEL:"fixture"}
        calls = []
        def docker(*args):
            calls.append(args)
            return json.dumps([{"Labels":{**labels, support.OWNER_LABEL:"changed"}}])
        with patch.object(support, "resources", return_value=[("volume","fixture_data","fixture_data")]), \
             patch.object(support, "docker", side_effect=docker), contextlib.redirect_stderr(io.StringIO()):
            self.assertTrue(support.cleanup("fixture", "owned"))
        self.assertEqual(calls, [("volume", "inspect", "fixture_data")])


if __name__ == "__main__":
    unittest.main(verbosity=2)
