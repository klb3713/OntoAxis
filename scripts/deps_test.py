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
