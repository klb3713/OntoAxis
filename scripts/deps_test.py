#!/usr/bin/env python3
"""deps.py 的回归测试（标准库 unittest，不联网）。

覆盖 2026-11-23 一致性修正的三处行为：
1. chart values 的 `tag` 元数据可被解析，且字段顺序无关；
2. `repository` 缺 `digest` 时仍产出条目（交给 check 报错，不静默消失）；
3. 例外 scope 前缀匹配与「占位 digest 不冒充已固定」的判定。

运行：python3 scripts/deps_test.py
"""

import importlib.util
import os
import sys
import tempfile
import unittest
from unittest import mock

# 测试是被导入执行的脚本，不落 __pycache__，避免污染仓库工作区。
sys.dont_write_bytecode = True

HERE = os.path.dirname(os.path.abspath(__file__))
SPEC = importlib.util.spec_from_file_location("deps", os.path.join(HERE, "deps.py"))
deps = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(deps)


def write_values(text):
    handle = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False, encoding="utf-8")
    handle.write(text)
    handle.close()
    return handle.name


class ExtractHelmTest(unittest.TestCase):
    def test_tag_before_digest(self):
        path = write_values(
            "components:\n"
            "  trino:\n"
            "    image:\n"
            "      repository: docker.io/trinodb/trino\n"
            "      tag: \"484\"\n"
            "      digest: sha256:" + "a" * 64 + "\n"
        )
        try:
            entries = deps.extract_helm(path)
        finally:
            os.unlink(path)
        self.assertEqual(len(entries), 1)
        self.assertEqual(entries[0]["tag"], "484")
        self.assertEqual(
            entries[0]["ref"], "docker.io/trinodb/trino@sha256:" + "a" * 64
        )

    def test_tag_after_digest(self):
        path = write_values(
            "components:\n"
            "  keycloak:\n"
            "    image:\n"
            "      repository: quay.io/keycloak/keycloak\n"
            "      digest: sha256:" + "b" * 64 + "\n"
            "      tag: \"26.0\"\n"
        )
        try:
            entries = deps.extract_helm(path)
        finally:
            os.unlink(path)
        self.assertEqual(entries[0]["tag"], "26.0")

    def test_repository_without_digest_still_emitted(self):
        path = write_values(
            "components:\n"
            "  postgres:\n"
            "    image:\n"
            "      repository: docker.io/library/postgres\n"
            "      tag: \"16-alpine\"\n"
        )
        try:
            entries = deps.extract_helm(path)
        finally:
            os.unlink(path)
        self.assertEqual(len(entries), 1)
        self.assertEqual(entries[0]["ref"], "docker.io/library/postgres")
        self.assertEqual(entries[0]["tag"], "16-alpine")

    def test_component_without_image_ignored(self):
        path = write_values("components:\n  empty:\n    enabled: false\n")
        try:
            entries = deps.extract_helm(path)
        finally:
            os.unlink(path)
        self.assertEqual(entries, [])

    def test_image_outside_components_still_collected(self):
        # jobs chart 的 backup.image 在 components 块外，不能成为门禁盲区。
        path = write_values(
            "global:\n"
            "  environment: kind\n"
            "backup:\n"
            "  schedule: \"0 2 * * *\"\n"
            "  image:\n"
            "    repository: docker.io/library/postgres\n"
            "    digest: sha256:" + "d" * 64 + "\n"
            "  secretName: ontoaxis-backup-secrets\n"
        )
        try:
            entries = deps.extract_helm(path)
        finally:
            os.unlink(path)
        self.assertEqual(len(entries), 1)
        self.assertIsNone(entries[0]["helm_component"])
        self.assertEqual(entries[0]["line"], 6)
        # 没有组件名时按镜像名回退，仍能对上 policy 里的 postgres。
        self.assertEqual(deps.component_key({**entries[0], "image": "docker.io/library/postgres"}), "postgres")

    def test_image_block_closed_by_sibling_key(self):
        path = write_values(
            "components:\n"
            "  postgres:\n"
            "    image:\n"
            "      repository: docker.io/library/postgres\n"
            "      digest: sha256:" + "e" * 64 + "\n"
            "    tag: not-an-image-tag\n"
        )
        try:
            entries = deps.extract_helm(path)
        finally:
            os.unlink(path)
        self.assertEqual(len(entries), 1)
        # 组件层的 tag 不是镜像元数据，不得被当成 image.tag。
        self.assertEqual(entries[0]["tag"], "")


class LockRenderTest(unittest.TestCase):
    def test_tag_round_trips_into_lock(self):
        entries = [
            {
                "source": "deploy/helm/ontoaxis-data/values.yaml",
                "line": 10,
                "component": "trino",
                "image": "docker.io/trinodb/trino",
                "tag": "484",
                "digest": "sha256:" + "c" * 64,
            }
        ]
        text = deps.render_lock(entries)
        self.assertIn('    tag: "484"', text)
        self.assertIn('    digest: "sha256:' + "c" * 64 + '"', text)
        # 无时间戳：同一输入必须逐字节稳定，否则锁文件无法 diff。
        self.assertEqual(text, deps.render_lock(entries))


class ResolveTest(unittest.TestCase):
    def test_docker_hub_digest(self):
        payload = {"digest": "sha256:" + "a" * 64}
        with mock.patch.object(deps, "http_json", return_value=payload) as http:
            digest = deps.registry_digest("docker.io/library/postgres", "16-alpine")
        self.assertEqual(digest, "sha256:" + "a" * 64)
        self.assertIn(
            "hub.docker.com/v2/repositories/library/postgres/tags/16-alpine",
            http.call_args[0][0],
        )

    def test_quay_digest(self):
        payload = {"tags": [{"manifest_digest": "sha256:" + "b" * 64}]}
        with mock.patch.object(deps, "http_json", return_value=payload) as http:
            digest = deps.registry_digest("quay.io/keycloak/keycloak", "26.0")
        self.assertEqual(digest, "sha256:" + "b" * 64)
        self.assertIn("specificTag=26.0", http.call_args[0][0])

    def test_quay_missing_tag_is_none(self):
        with mock.patch.object(deps, "http_json", return_value={"tags": []}):
            self.assertIsNone(deps.registry_digest("quay.io/keycloak/keycloak", "0.0.0"))

    def test_docker_hub_missing_digest_is_none(self):
        with mock.patch.object(deps, "http_json", return_value={}):
            self.assertIsNone(deps.registry_digest("docker.io/library/postgres", "0.0.0"))

    def test_self_built_images_are_not_queried(self):
        with mock.patch.object(deps, "http_json", side_effect=AssertionError("must not query")):
            self.assertIsNone(deps.registry_digest("ghcr.io/ontoaxis/core", "abc123"))


class ExceptionCoverageTest(unittest.TestCase):
    policy = {
        "exceptions": [
            {"id": "helm-placeholder-digests", "scope": "deploy/helm/", "expires": "2026-12-31"}
        ]
    }

    def test_scope_prefix_matches(self):
        entry = {"source": "deploy/helm/ontoaxis-data/values.yaml"}
        self.assertIsNotNone(deps.exception_covering(self.policy, entry))

    def test_non_matching_source_not_covered(self):
        entry = {"source": "deploy/compose/compose.yaml"}
        self.assertIsNone(deps.exception_covering(self.policy, entry))

    def test_empty_scope_never_matches(self):
        policy = {"exceptions": [{"id": "x", "scope": "", "expires": "2026-12-31"}]}
        entry = {"source": "deploy/helm/ontoaxis-data/values.yaml"}
        self.assertIsNone(deps.exception_covering(policy, entry))


if __name__ == "__main__":
    unittest.main(verbosity=2)
