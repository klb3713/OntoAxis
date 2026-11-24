#!/usr/bin/env python3
"""Render a reviewed kind GitOps root tree without Git writes or cluster access."""
import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

import yaml


APPS = {
    "00-namespaces.yaml": "namespaces", "10-secrets.yaml": "secrets",
    "20-platform.yaml": "platform", "30-data.yaml": "data", "40-app.yaml": "app",
    "50-edge.yaml": "edge", "60-observability.yaml": "observability", "70-jobs.yaml": "jobs",
}
APPS_DIR = "deploy/argocd/apps"
GIT_URL = "http://ontoaxis-git.argocd.svc.cluster.local:8080/ontoaxis.git"
ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checkout", type=Path, required=True)
    parser.add_argument("--content-revision", required=True)
    parser.add_argument("--env", choices=("kind", "kind-small"), required=True)
    parser.add_argument("--out", type=Path, required=True,
                        help="new directory for the standalone root tree (does not overwrite existing files)")
    args = parser.parse_args()
    revision = args.content_revision
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        parser.error("--content-revision must be a full lowercase Git SHA")
    checkout = args.checkout.resolve(strict=True)
    output = args.out.absolute()
    parent = output.parent.resolve(strict=True)
    output = parent / output.name
    if not output.name or output.exists() or output.is_symlink() or output.is_relative_to(checkout) or output.is_relative_to(ROOT):
        parser.error("--out must be a new directory outside both the checkout and the source workspace")
    git_env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
    git_env.update(GIT_NO_LAZY_FETCH="1", GIT_ALLOW_PROTOCOL="file", GIT_NO_REPLACE_OBJECTS="1", GIT_OPTIONAL_LOCKS="0")

    def git(*flags):
        return subprocess.run(["git", "-C", str(checkout), *flags], env=git_env, text=True,
                              check=True, capture_output=True, timeout=15).stdout

    if git("cat-file", "-t", revision).strip() != "commit":
        raise ValueError("content revision is not a commit")

    def item_type(path, expected):
        if git("cat-file", "-t", f"{revision}:{path}").strip() != expected:
            raise ValueError(f"content revision has no {expected} at {path}")

    overlays = ["deploy/values/common.yaml", f"deploy/values/{args.env}.yaml"]
    for path in overlays:
        item_type(path, "blob")
    documents = {}
    for filename, component in APPS.items():
        # Templates come only from the selected content commit, not the working tree.
        ref = APPS_DIR + "/" + filename
        item_type(ref, "blob")
        doc = yaml.safe_load(git("show", f"{revision}:{ref}"))
        if (doc["kind"] != "Application" or doc["metadata"]["name"] != f"ontoaxis-{component}"
                or doc["metadata"]["namespace"] != "argocd" or doc["spec"]["project"] != "ontoaxis"):
            raise ValueError(f"unexpected Application identity: {filename}")
        doc["metadata"]["labels"]["ontoaxis.io/environment"] = args.env
        spec = doc["spec"]
        approved_source = {"repoURL": GIT_URL, "targetRevision": revision}
        if component in {"platform", "data", "app", "observability", "jobs"}:
            chart = f"deploy/helm/ontoaxis-{component}"
            item_type(chart + "/Chart.yaml", "blob")
            spec.pop("source", None)
            spec["sources"] = [dict(approved_source, ref="values"),
                               dict(approved_source, path=chart, helm={"valueFiles": ["$values/" + p for p in overlays]})]
        else:
            paths = {"namespaces": "deploy/argocd/manifests/namespaces",
                     "secrets": "deploy/argocd/secrets/kind", "edge": "deploy/edge/generated"}
            path = paths[component]
            item_type(path, "tree")
            directory = {"recurse": component != "edge"}
            if component == "edge":
                directory["include"] = "envoy-httproute.yaml"
                item_type(path + "/envoy-httproute.yaml", "blob")
            spec.pop("sources", None)
            spec["source"] = dict(approved_source, path=path, directory=directory)
        documents[filename] = yaml.safe_dump(doc, sort_keys=False)
    # Build files in a temporary sibling, then exclusively reserve the output path.
    # An existing output is never erased or reused, including on failed publication.
    with tempfile.TemporaryDirectory(prefix=".gitops-root-", dir=parent) as temp:
        root = Path(temp)
        target = root / APPS_DIR
        target.mkdir(parents=True)
        for name, doc in documents.items():
            (target / name).write_text(doc, encoding="utf-8")
        output.mkdir(mode=0o700, exist_ok=False)
        shutil.move(str(root / "deploy"), output / "deploy")
    print(f"Rendered {len(documents)} Applications from {revision}; review and publish separately: {output}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, subprocess.SubprocessError, yaml.YAMLError, KeyError, TypeError, ValueError, AttributeError) as exc:
        sys.exit(f"error: could not render gitops root: {exc}")
