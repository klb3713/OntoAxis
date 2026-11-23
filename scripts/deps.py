#!/usr/bin/env python3
"""依赖版本治理工具。

子命令：
  lock   从部署资产抽取镜像引用，生成可 diff 的锁文件
  check  离线校验：锁文件新鲜度、latest 禁令、策略覆盖、例外到期、digest 棘轮
  audit  联网查询上游最新版本与 EOL，输出 markdown 报告

只依赖 python3 标准库。policy.yaml 只支持受限子集：顶层标量、两层嵌套
（components/exceptions/defaults）、标量值、[a, b] 行内列表与 # 注释。
解析失败会带行号报错，不静默忽略。
"""

import argparse
import datetime
import glob
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
POLICY_PATH = os.path.join(ROOT, "deploy", "deps", "policy.yaml")
LOCK_PATH = os.path.join(ROOT, "deploy", "deps", "lock.yaml")
HTTP_TIMEOUT = 8


class PolicyError(Exception):
    pass


# --------------------------------------------------------------------------
# 基础工具
# --------------------------------------------------------------------------


def rel(path):
    return os.path.relpath(path, ROOT)


def read_lines(path):
    with open(path, "r", encoding="utf-8") as handle:
        return handle.readlines()


def strip_comment(text):
    quote = None
    for index, char in enumerate(text):
        if char in "\"'":
            quote = None if quote == char else (quote or char)
        elif char == "#" and quote is None:
            return text[:index]
    return text


def parse_scalar(raw):
    value = raw.strip()
    if value.startswith("[") and value.endswith("]"):
        inner = value[1:-1].strip()
        return [parse_scalar(part) for part in inner.split(",")] if inner else []
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    if value in ("true", "True"):
        return True
    if value in ("false", "False"):
        return False
    if re.fullmatch(r"-?\d+", value):
        return int(value)
    return value


def split_ref(ref):
    """把 image 引用拆成 (repo, tag, digest)。"""
    digest = None
    if "@" in ref:
        ref, digest = ref.split("@", 1)
    tag = None
    last = ref.rsplit("/", 1)[-1]
    if ":" in last:
        ref, tag = ref.rsplit(":", 1)
    return ref, tag, digest


def normalize_image(image):
    """去掉 docker.io 前缀与 library/ 命名空间，便于和 Renovate 包名对齐。"""
    for prefix in ("docker.io/", "index.docker.io/"):
        if image.startswith(prefix):
            image = image[len(prefix) :]
    if image.startswith("library/"):
        image = image[len("library/") :]
    return image


def component_key(entry):
    """策略文件里使用的组件名。"""
    if entry.get("helm_component"):
        return entry["helm_component"]
    if entry.get("service"):
        return entry["service"]
    name = entry["image"].rsplit("/", 1)[-1]
    return name


# --------------------------------------------------------------------------
# 抽取
# --------------------------------------------------------------------------


def extract_compose(path):
    entries = []
    service = None
    for lineno, raw in enumerate(read_lines(path), 1):
        line = strip_comment(raw).rstrip()
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip())
        image = re.match(r"^\s*image:\s*(\S+)\s*$", line)
        if image:
            entries.append({"source": rel(path), "line": lineno, "service": service, "ref": image.group(1)})
            continue
        top = re.match(r"^[A-Za-z0-9_.-]+:\s*$", line)
        if top and indent == 0:
            service = None
            continue
        svc = re.match(r"^  ([A-Za-z0-9_.-]+):\s*$", line)
        if svc and indent == 2:
            service = svc.group(1)
    return entries


def extract_dockerfile(path):
    entries = []
    stages = set()
    for lineno, raw in enumerate(read_lines(path), 1):
        line = strip_comment(raw).rstrip()
        match = re.match(r"^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)(?:\s+AS\s+(\S+))?", line, re.IGNORECASE)
        if not match:
            continue
        ref, alias = match.group(1), match.group(2)
        if alias:
            stages.add(alias.lower())
        if ref.lower() in ("scratch",) or ref.lower() in stages or "$" in ref:
            continue
        entries.append({"source": rel(path), "line": lineno, "service": None, "ref": ref})
    return entries


def extract_helm(path):
    """解析 chart values 中的 image 映射（`image:` 块及其 repository/tag/digest）。

    组件名取 `components:` 下的块名；在 `components:` 之外的 image 映射（例如
    jobs chart 的 `backup.image`）不归属组件，交给 `component_key` 按镜像名回退，
    从而同样进入锁文件、审计与门禁，不再出现「块外镜像不被检查」的盲区。

    `tag` 只是元数据：模板仍按 `repository@digest` 渲染，tag 供审计与 Renovate
    判断版本。字段顺序不影响结果。`repository` 存在但缺 `digest` 时同样产出条目，
    由 check 报 E_NOTAG/W_NODIGEST，避免「缺 digest 的镜像从清单里消失」。
    """
    entries = []
    in_components = False
    component = None
    image = None
    for lineno, raw in enumerate(read_lines(path), 1):
        line = strip_comment(raw).rstrip()
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip())
        text = line.strip()

        if indent == 0:
            in_components = text == "components:"
            component = None
            image = None
            continue

        if in_components and indent == 2:
            comp = re.match(r"^([A-Za-z0-9_.-]+):\s*$", text)
            if comp:
                component = comp.group(1)
                image = None
                continue

        if text == "image:":
            image = {
                "component": component if in_components else None,
                "repository": None,
                "tag": "",
                "digest": None,
                "line": lineno,
                "indent": indent,
            }
            entries.append(image)
            continue

        if image is None:
            continue
        if indent <= image["indent"]:
            image = None
            continue

        field = re.match(r"^(repository|tag|digest):\s*(\S+)\s*$", text)
        if not field:
            continue
        key, raw_value = field.group(1), field.group(2)
        value = str(parse_scalar(raw_value))
        if key == "repository" and image["repository"] is None:
            image["repository"] = value
            image["line"] = lineno
        elif key == "tag" and not image["tag"]:
            image["tag"] = value
        elif key == "digest" and image["digest"] is None:
            image["digest"] = value

    found = []
    for block in entries:
        if not block["repository"]:
            continue
        ref = block["repository"]
        if block["digest"]:
            ref += "@" + block["digest"]
        found.append(
            {
                "source": rel(path),
                "line": block["line"],
                "service": None,
                "helm_component": block["component"],
                "ref": ref,
                "tag": block["tag"],
            }
        )
    return found


def extract_makefile(path):
    entries = []
    for lineno, raw in enumerate(read_lines(path), 1):
        line = strip_comment(raw).rstrip()
        if "docker run" not in line:
            continue
        for match in re.finditer(r"\b([a-z0-9][a-z0-9._/-]*:[A-Za-z0-9][A-Za-z0-9._-]*)\b", line):
            ref = match.group(1)
            if ref.startswith("/"):
                continue
            entries.append({"source": "Makefile", "line": lineno, "service": None, "ref": ref})
    return entries


def collect_entries():
    found = []
    for pattern, extractor in (
        ("deploy/compose/**/*.yaml", extract_compose),
        ("deploy/**/Dockerfile*", extract_dockerfile),
        ("deploy/helm/*/values*.yaml", extract_helm),
    ):
        for path in sorted(glob.glob(os.path.join(ROOT, pattern), recursive=True)):
            if os.path.basename(path).startswith("."):
                continue
            found.extend(extractor(path))
    makefile = os.path.join(ROOT, "Makefile")
    if os.path.exists(makefile):
        found.extend(extract_makefile(makefile))

    entries = []
    for item in found:
        image, tag, digest = split_ref(item["ref"])
        entries.append(
            {
                "source": item["source"],
                "line": item["line"],
                "component": component_key({**item, "image": image}),
                "image": image,
                # Helm values 的 tag 是独立元数据字段，不写在引用里。
                "tag": tag or item.get("tag") or "",
                "digest": digest or "",
            }
        )
    entries.sort(key=lambda e: (e["source"], e["line"], e["image"]))
    return entries


def render_lock(entries):
    lines = ["# 由 `make deps-lock` 生成，请勿手工编辑。", "entries:"]
    for entry in entries:
        lines.append("  - source: %s" % entry["source"])
        lines.append("    line: %d" % entry["line"])
        lines.append("    component: %s" % entry["component"])
        lines.append("    image: %s" % entry["image"])
        lines.append('    tag: "%s"' % entry["tag"])
        lines.append('    digest: "%s"' % entry["digest"])
    return "\n".join(lines) + "\n"


# --------------------------------------------------------------------------
# 策略解析
# --------------------------------------------------------------------------


def load_policy():
    if not os.path.exists(POLICY_PATH):
        raise PolicyError("缺少 %s" % rel(POLICY_PATH))
    data = {"components": {}, "exceptions": [], "defaults": {}}
    section = None
    current = None
    for lineno, raw in enumerate(read_lines(POLICY_PATH), 1):
        line = strip_comment(raw).rstrip()
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip())
        text = line.strip()

        if indent == 0:
            match = re.match(r"^([A-Za-z0-9_.-]+):\s*(.*)$", text)
            if not match:
                raise PolicyError("%s:%d 无法解析的顶层行: %r" % (rel(POLICY_PATH), lineno, text))
            key, value = match.group(1), match.group(2).strip()
            current = None
            if value:
                data[key] = parse_scalar(value)
                section = None
            else:
                section = key
            continue

        if section == "components":
            if indent == 2:
                match = re.match(r"^([A-Za-z0-9_.-]+):\s*$", text)
                if not match:
                    raise PolicyError("%s:%d 组件名格式错误: %r" % (rel(POLICY_PATH), lineno, text))
                current = match.group(1)
                data["components"].setdefault(current, {})
            elif indent == 4 and current:
                match = re.match(r"^([A-Za-z0-9_.-]+):\s*(.+)$", text)
                if not match:
                    raise PolicyError("%s:%d 组件字段格式错误: %r" % (rel(POLICY_PATH), lineno, text))
                data["components"][current][match.group(1)] = parse_scalar(match.group(2))
            else:
                raise PolicyError("%s:%d 组件块缩进错误: %r" % (rel(POLICY_PATH), lineno, text))
            continue

        if section == "defaults":
            if indent == 2:
                match = re.match(r"^([A-Za-z0-9_.-]+):\s*(.+)$", text)
                if not match:
                    raise PolicyError("%s:%d defaults 字段格式错误: %r" % (rel(POLICY_PATH), lineno, text))
                data["defaults"][match.group(1)] = parse_scalar(match.group(2))
            else:
                raise PolicyError("%s:%d defaults 缩进错误: %r" % (rel(POLICY_PATH), lineno, text))
            continue

        if section == "exceptions":
            if indent == 2 and text.startswith("- "):
                match = re.match(r"^-\s+([A-Za-z0-9_.-]+):\s*(.+)$", text)
                if not match:
                    raise PolicyError("%s:%d 例外首字段格式错误: %r" % (rel(POLICY_PATH), lineno, text))
                current = {match.group(1): parse_scalar(match.group(2))}
                data["exceptions"].append(current)
            elif indent == 4 and isinstance(current, dict):
                match = re.match(r"^([A-Za-z0-9_.-]+):\s*(.+)$", text)
                if not match:
                    raise PolicyError("%s:%d 例外字段格式错误: %r" % (rel(POLICY_PATH), lineno, text))
                current[match.group(1)] = parse_scalar(match.group(2))
            else:
                raise PolicyError("%s:%d 例外缩进错误: %r" % (rel(POLICY_PATH), lineno, text))
            continue

        raise PolicyError("%s:%d 不在已知块内: %r" % (rel(POLICY_PATH), lineno, text))

    if not data["components"]:
        raise PolicyError("%s 未声明任何组件" % rel(POLICY_PATH))
    return data


def policy_for(policy, name):
    defaults = policy.get("defaults", {})
    return {**defaults, **policy["components"].get(name, {})}


# --------------------------------------------------------------------------
# check
# --------------------------------------------------------------------------


def parse_date(value):
    try:
        return datetime.date.fromisoformat(str(value))
    except (TypeError, ValueError):
        return None


def floating_tag(tag):
    """整数 tag 视为精确构建号（如 Trino 484），其余 major/minor 视为浮动。"""
    if re.fullmatch(r"\d+", tag or ""):
        return False
    return bool(re.fullmatch(r"v?\d+(\.\d+)?(-\w+)?", tag or ""))


def exception_covering(policy, entry):
    """返回覆盖该引用的例外（按 scope 前缀匹配），没有则返回 None。"""
    for exception in policy["exceptions"]:
        scope = str(exception.get("scope") or "")
        if scope and entry["source"].startswith(scope):
            return exception
    return None


def cmd_lock(args):
    entries = collect_entries()
    text = render_lock(entries)
    if args.write:
        os.makedirs(os.path.dirname(LOCK_PATH), exist_ok=True)
        with open(LOCK_PATH, "w", encoding="utf-8") as handle:
            handle.write(text)
        print("wrote %s (%d entries)" % (rel(LOCK_PATH), len(entries)))
    else:
        sys.stdout.write(text)
    return 0


def check_renovate(policy, entries, errors, warnings):
    """renovate.json 的 P0 名单必须覆盖 policy.yaml 的全部 p0 组件。"""
    path = os.path.join(ROOT, "renovate.json")
    if not os.path.exists(path):
        errors.append("error[E_RENOVATE] 缺少 renovate.json")
        return
    try:
        with open(path, "r", encoding="utf-8") as handle:
            config = json.load(handle)
    except ValueError as exc:
        errors.append("error[E_RENOVATE] renovate.json 不是合法 JSON: %s" % exc)
        return

    rules = config.get("packageRules") or []
    p0_rule = next((rule for rule in rules if rule.get("groupName") == "platform-p0"), None)
    if p0_rule is None:
        errors.append("error[E_RENOVATE] renovate.json 缺少 groupName=platform-p0 的规则")
        return
    if p0_rule.get("automerge") is not False:
        errors.append("error[E_RENOVATE] platform-p0 规则必须显式 automerge=false")

    names = {normalize_image(name) for name in (p0_rule.get("matchPackageNames") or [])}
    images = {entry["component"]: normalize_image(entry["image"]) for entry in entries}
    for name, cfg in policy["components"].items():
        if str(cfg.get("tier")) != "p0":
            continue
        image = images.get(name)
        if not image or image.startswith("ghcr.io/ontoaxis/"):
            continue
        if image not in names:
            errors.append(
                "error[E_RENOVATE] p0 组件 %s（%s）未出现在 platform-p0 规则中" % (name, image)
            )
    for name in sorted(names):
        if name not in images.values():
            warnings.append("warn[W_RENOVATE] platform-p0 规则中的 %s 未在仓库中引用" % name)


def cmd_check(args):
    errors = []
    warnings = []
    try:
        policy = load_policy()
    except PolicyError as exc:
        print("error[E_POLICY] %s" % exc)
        return 2

    today = datetime.date.today()
    ratchet = parse_date(policy.get("digest_required_from"))
    if ratchet is None:
        errors.append("error[E_POLICY] 缺少 digest_required_from 日期")
        ratchet = datetime.date(2100, 1, 1)

    for exception in policy["exceptions"]:
        expires = parse_date(exception.get("expires"))
        if expires is None:
            errors.append("error[E_EXCEPTION] 例外 %s 缺少 expires" % exception.get("id"))
        elif expires < today:
            errors.append(
                "error[E_EXCEPTION] 例外 %s 已于 %s 过期（owner: %s）"
                % (exception.get("id"), expires, exception.get("owner", "?"))
            )

    entries = collect_entries()
    check_renovate(policy, entries, errors, warnings)
    expected = render_lock(entries)
    if not os.path.exists(LOCK_PATH):
        errors.append("error[E_LOCK_MISSING] 缺少 %s，请执行 make deps-lock" % rel(LOCK_PATH))
    else:
        with open(LOCK_PATH, "r", encoding="utf-8") as handle:
            committed = handle.read()
        if committed != expected:
            errors.append("error[E_LOCK_STALE] 锁文件与仓库不一致，请执行 make deps-lock")

    exception_covered = {}
    for entry in entries:
        name = entry["component"]
        cfg = policy_for(policy, name)
        if name not in policy["components"]:
            errors.append(
                "error[E_UNCLASSIFIED] %s:%d %s 未在 policy.yaml 声明 tier/owner"
                % (entry["source"], entry["line"], name)
            )
            continue

        tag = entry["tag"]
        if tag == "latest":
            errors.append("error[E_LATEST] %s:%d %s 使用 latest" % (entry["source"], entry["line"], name))
        elif not tag and not entry["digest"]:
            errors.append(
                "error[E_NOTAG] %s:%d %s 既无 tag 也无 digest" % (entry["source"], entry["line"], name)
            )
        elif tag and floating_tag(tag):
            warnings.append(
                "warn[W_FLOATING] %s:%d %s 的 tag %r 为浮动 major/minor"
                % (entry["source"], entry["line"], name, tag)
            )

        covering = exception_covering(policy, entry)
        if covering is not None:
            key = str(covering.get("id"))
            exception_covered[key] = exception_covered.get(key, 0) + 1

        # Helm 只写 digest 时审计无法判定版本（DEPENDENCIES.md §6 要求补 tag）。
        if entry["digest"] and not tag and entry["source"].startswith("deploy/helm/"):
            warnings.append(
                "warn[W_NOTAG] %s:%d %s 只有 digest 没有 tag，审计无法判定版本"
                % (entry["source"], entry["line"], name)
            )

        if not entry["digest"]:
            if today >= ratchet:
                errors.append(
                    "error[E_NODIGEST] %s:%d %s 缺少 digest（棘轮日期 %s 已到）"
                    % (entry["source"], entry["line"], name, ratchet)
                )
            else:
                warnings.append(
                    "warn[W_NODIGEST] %s:%d %s 缺少 digest（%s 起强制）"
                    % (entry["source"], entry["line"], name, ratchet)
                )

    # 例外覆盖的条目本身不是错误，但必须可见：否则「大量未知」会被静默当作已固定。
    for exception in policy["exceptions"]:
        key = str(exception.get("id"))
        count = exception_covered.get(key, 0)
        if count:
            warnings.append(
                "warn[W_EXCEPTION_COVERED] 例外 %s 覆盖 %s 下 %d 条引用（到期 %s），到期后未修复即阻塞"
                % (key, exception.get("scope", "?"), count, exception.get("expires", "?"))
            )

    for line in errors:
        print(line)
    if args.verbose:
        for line in warnings:
            print(line)

    print(
        "summary: entries=%d components=%d exception_covered=%d errors=%d warnings=%d ratchet=%s"
        % (
            len(entries),
            len(policy["components"]),
            sum(exception_covered.values()),
            len(errors),
            len(warnings),
            ratchet,
        )
    )
    return 1 if errors else 0


# --------------------------------------------------------------------------
# audit
# --------------------------------------------------------------------------


def http_json(url):
    request = urllib.request.Request(url, headers={"User-Agent": "ontoaxis-deps-audit/1"})
    with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT) as response:
        return json.loads(response.read().decode("utf-8"))


def version_key(tag):
    match = re.match(r"^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?", tag or "")
    if not match:
        return None
    return tuple(int(part or 0) for part in match.groups())


def latest_docker_hub(image):
    path = normalize_image(image)
    if "/" not in path:
        path = "library/" + path
    url = "https://hub.docker.com/v2/repositories/%s/tags?page_size=100&ordering=last_updated" % path
    payload = http_json(url)
    candidates = []
    for tag in payload.get("results", []):
        name = tag.get("name", "")
        if re.search(r"(rc|beta|alpha|nightly|dev|snapshot)", name, re.IGNORECASE):
            continue
        if not re.fullmatch(r"v?\d+(\.\d+){0,2}", name) or re.fullmatch(r"20\d{6}", name):
            continue
        candidates.append((version_key(name), name))
    return max(candidates)[1] if candidates else None


def latest_quay(image):
    path = image.split("quay.io/", 1)[-1]
    url = "https://quay.io/api/v1/repository/%s/tag/?limit=100&onlyActiveTags=true" % path
    payload = http_json(url)
    candidates = []
    for tag in payload.get("tags", []):
        name = tag.get("name", "")
        if re.search(r"(rc|beta|alpha|nightly|dev|snapshot)", name, re.IGNORECASE):
            continue
        if not re.fullmatch(r"v?\d+(\.\d+){0,2}", name) or re.fullmatch(r"20\d{6}", name):
            continue
        candidates.append((version_key(name), name))
    return max(candidates)[1] if candidates else None


def eol_for(slug, major):
    try:
        cycles = http_json("https://endoflife.date/api/%s.json" % slug)
    except (urllib.error.URLError, ValueError, OSError):
        return None
    for cycle in cycles:
        if str(cycle.get("cycle")) == str(major):
            return cycle.get("eol")
    return None


def registry_digest(image, tag):
    """查询 registry 返回 tag 对应的 manifest digest；无法确认时返回 None。"""
    if image.startswith("ghcr.io/ontoaxis/"):
        return None
    if image.startswith("quay.io/"):
        repo = image.split("quay.io/", 1)[1]
        payload = http_json(
            "https://quay.io/api/v1/repository/%s/tag/?specificTag=%s&limit=5"
            % (repo, urllib.parse.quote(tag))
        )
        tags = payload.get("tags") or []
        return tags[0].get("manifest_digest") if tags else None
    path = normalize_image(image)
    if "/" not in path:
        path = "library/" + path
    payload = http_json(
        "https://hub.docker.com/v2/repositories/%s/tags/%s"
        % (path, urllib.parse.quote(tag))
    )
    return payload.get("digest")


def cmd_resolve(args):
    """把 lock 中的 tag 解析成 digest，输出可直接用于 Helm values 的映射表。

    只读查询，不写仓库文件；查询不到记为 unknown，不推测。用于 Helm 占位 digest
    回填前先拿到真实 digest，减少人工逐个查 registry。
    """
    rows = {}
    for entry in collect_entries():
        if not entry["tag"]:
            continue
        key = (entry["image"], entry["tag"])
        row = rows.setdefault(key, {"components": set(), "sources": set()})
        row["components"].add(entry["component"])
        row["sources"].add(entry["source"])

    # --ref 用于 Helm 侧还没有 tag 的组件：先按拟定的 image:tag 解析 digest。
    for raw in args.ref or []:
        image, tag, digest = split_ref(raw)
        if not tag or digest:
            raise SystemExit("error[E_REF] --ref 需要 image:tag 形式，收到 %r" % raw)
        key = (image, tag)
        row = rows.setdefault(key, {"components": set(), "sources": set()})
        row["components"].add("(指定)")
        row["sources"].add("--ref")

    out = [
        "# tag → digest 映射",
        "",
        "- 生成时间：%s" % datetime.datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z"),
        "- 来源：`deploy/deps/lock.yaml` + Docker Hub / Quay manifest 查询",
        "- 说明：digest 为 registry 返回的 manifest（多架构为 image index）digest；查询失败记 unknown，不推测。",
        "",
        "| 组件 | 镜像 | tag | digest | 来源 |",
        "|---|---|---|---|---|",
    ]
    unknown = 0
    for (image, tag), row in sorted(rows.items()):
        try:
            digest = registry_digest(image, tag)
        except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError):
            digest = None
        if not digest:
            digest = "unknown"
            unknown += 1
        out.append(
            "| %s | `%s` | `%s` | `%s` | %s |"
            % (
                ",".join(sorted(row["components"])),
                image,
                tag,
                digest,
                ",".join(sorted(row["sources"])),
            )
        )
    out.append("")
    out.append("共 %d 条，unknown %d 条。" % (len(rows), unknown))
    text = "\n".join(out) + "\n"

    if args.out:
        target = args.out if os.path.isabs(args.out) else os.path.join(ROOT, args.out)
        os.makedirs(os.path.dirname(target) or ".", exist_ok=True)
        with open(target, "w", encoding="utf-8") as handle:
            handle.write(text)
        print("wrote %s (%d rows, %d unknown)" % (rel(target), len(rows), unknown))
    else:
        sys.stdout.write(text)
    return 0


def cmd_audit(args):
    try:
        policy = load_policy()
    except PolicyError as exc:
        print("error[E_POLICY] %s" % exc, file=sys.stderr)
        return 2

    grouped = {}
    for entry in collect_entries():
        grouped.setdefault(normalize_image(entry["image"]), []).append(entry)

    def covered_note(group):
        ids = []
        for entry in group:
            exception = exception_covering(policy, entry)
            if exception is not None:
                ids.append(str(exception.get("id")))
        return "例外 %s" % ",".join(sorted(set(ids))) if ids else ""

    def digest_state(group):
        """区分「真实 digest」与「只被例外覆盖的占位 digest」，不把占位算成已固定。"""
        real = [
            entry
            for entry in group
            if entry["digest"] and exception_covering(policy, entry) is None
        ]
        if real:
            return "有"
        if any(entry["digest"] for entry in group):
            return "例外覆盖（未验证）"
        return "无"

    rows = []
    for image in sorted(grouped):
        group = grouped[image]
        tagged = [entry for entry in group if entry["tag"]] or group
        pick = tagged[0]
        name = pick["component"]
        cfg = policy_for(policy, name)
        tag = pick["tag"] or "（无 tag）"
        current = version_key(pick["tag"])
        latest = None
        note = covered_note(group)
        if image.startswith("ghcr.io/ontoaxis/"):
            note = ("自有镜像（CI 构建），不查上游; " + note).rstrip("; ")
        else:
            try:
                latest = latest_quay(image) if image.startswith("quay.io/") else latest_docker_hub(image)
            except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError) as exc:
                note = ("查询失败: %s; " % str(exc)[:60]) + note
        drift = "unknown"
        if current and latest:
            newest = version_key(latest)
            if newest and newest > current:
                if newest[0] > current[0]:
                    drift = "落后 major"
                elif newest[1] > current[1]:
                    drift = "落后 minor"
                else:
                    drift = "落后 patch"
            elif newest == current:
                drift = "一致"
        eol = ""
        slug = cfg.get("eol_source")
        if slug and current:
            value = eol_for(str(slug), current[0])
            if value is True:
                eol = "已 EOL"
            elif value:
                eol = str(value)
        rows.append(
            {
                "name": name,
                "tag": tag,
                "digest": digest_state(group),
                "latest": latest or "unknown",
                "drift": drift,
                "tier": cfg.get("tier", "?"),
                "owner": cfg.get("owner", "?"),
                "review_by": cfg.get("review_by", "?"),
                "eol": eol,
                "note": note.rstrip("; "),
            }
        )

    tier_order = {"p0": 0, "p1": 1, "p2": 2, "?": 3}
    rows.sort(key=lambda row: (tier_order.get(str(row["tier"]), 9), row["name"]))

    now = datetime.datetime.now().astimezone().strftime("%Y-%m-%dT%H:%M:%S%z")
    out = []
    out.append("# 依赖版本审计报告")
    out.append("")
    out.append("- 生成时间：%s" % now)
    out.append("- 来源：`deploy/deps/policy.yaml` + `deploy/deps/lock.yaml`")
    out.append("- 说明：上游最新来自 Docker Hub / Quay 公开 API；无法查询记为 unknown，不推测。")
    out.append("")
    out.append("| 组件 | 当前 | digest | 上游最新 | 差距 | tier | owner | review_by | EOL | 备注 |")
    out.append("|---|---|---|---|---|---|---|---|---|---|")
    for row in rows:
        out.append(
            "| %s | `%s` | %s | %s | %s | %s | %s | %s | %s | %s |"
            % (
                row["name"],
                row["tag"],
                row["digest"],
                row["latest"],
                row["drift"],
                row["tier"],
                row["owner"],
                row["review_by"],
                row["eol"],
                row["note"],
            )
        )
    text = "\n".join(out) + "\n"

    if args.out:
        target = args.out if os.path.isabs(args.out) else os.path.join(ROOT, args.out)
        os.makedirs(os.path.dirname(target) or ".", exist_ok=True)
        with open(target, "w", encoding="utf-8") as handle:
            handle.write(text)
        print("wrote %s (%d components)" % (rel(target), len(rows)))
    else:
        sys.stdout.write(text)
    return 0


def main(argv):
    parser = argparse.ArgumentParser(description="依赖版本治理工具")
    sub = parser.add_subparsers(dest="command", required=True)

    lock = sub.add_parser("lock", help="生成锁文件")
    lock.add_argument("--write", action="store_true", help="写回 deploy/deps/lock.yaml")
    lock.set_defaults(func=cmd_lock)

    check = sub.add_parser("check", help="离线校验")
    check.add_argument("--verbose", action="store_true", help="显示警告")
    check.set_defaults(func=cmd_check)

    resolve = sub.add_parser("resolve", help="把 lock 中的 tag 解析成 digest（联网）")
    resolve.add_argument("--out", help="写入指定文件")
    resolve.add_argument(
        "--ref",
        action="append",
        metavar="IMAGE:TAG",
        help="额外解析未在 lock 中出现的 image:tag（可重复）",
    )
    resolve.set_defaults(func=cmd_resolve)

    audit = sub.add_parser("audit", help="联网审计并输出报告")
    audit.add_argument("--out", help="写入指定文件")
    audit.set_defaults(func=cmd_audit)

    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))