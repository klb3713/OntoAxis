#!/usr/bin/env bash
# delivery-render-check.sh — render every chart against every environment overlay
# and statically validate the result.
#
# Behaviour:
#   * If `helm` is present: for each chart x values file run `helm template`
#     twice, compare sha256 of the two renders (determinism check), and run
#     `kubeconform` when available.
#   * If `helm` is absent: print `SKIP helm not installed` and continue. Charts
#     are still validated statically:
#       - every YAML file parses (PyYAML yaml.safe_load; Ruby YAML fallback;
#         grep-only fallback when neither parser is installed)
#       - fail on obvious `repository: ...:latest` or missing/malformed
#         `digest:` values in the values overlays.
#   * Exit 0 when everything passes (including the helm-absent SKIP path),
#     non-zero when a real validation/render failure is found.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELM_DIR="$ROOT/deploy/helm"
VALUES_DIR="$ROOT/deploy/values"

CHARTS=(ontoaxis-platform ontoaxis-data ontoaxis-app ontoaxis-observability ontoaxis-jobs)
ENVS=(kind kind-small staging-like prod-like)

status=0

sha() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "no-sha-tool"
  fi
}

# ---------------------------------------------------------------- yaml checks
python_has_yaml() {
  command -v python3 >/dev/null 2>&1 && python3 -c 'import yaml' >/dev/null 2>&1
}

validate_with_python() {
  local files=("$@")
  python3 - "${files[@]}" <<'PY'
import sys, re
try:
    import yaml
except Exception as exc:  # pragma: no cover
    print("SKIP python yaml import failed: %s" % exc)
    sys.exit(2)
bad = 0
for path in sys.argv[1:]:
    # 1) parse (multi-document tolerated)
    try:
        with open(path, "r", encoding="utf-8") as fh:
            list(yaml.safe_load_all(fh))
    except Exception as exc:
        print("FAIL yaml parse %s: %s" % (path, exc))
        bad = 1
        continue
    # 2) image hygiene on values-style files
    with open(path, "r", encoding="utf-8") as fh:
        for i, line in enumerate(fh, 1):
            m = re.match(r"\s*repository:\s*(.*)$", line)
            if m:
                v = m.group(1).strip().strip("\"'")
                if ":latest" in v:
                    print("FAIL :latest repository %s:%d: %s" % (path, i, v))
                    bad = 1
            d = re.match(r"\s*digest:\s*(.*)$", line)
            if d:
                v = d.group(1).strip().strip("\"'")
                if v == "":
                    print("FAIL empty digest %s:%d" % (path, i))
                    bad = 1
                elif not re.fullmatch(r"sha256:[0-9a-f]{64}", v):
                    print("FAIL malformed digest %s:%d: %s" % (path, i, v))
                    bad = 1
            # 3) rendered manifests carry `image: repo@sha256:<64hex>`
            im = re.match(r"\s*image:\s*(\S.*)$", line)
            if im:
                v = im.group(1).strip().strip("\"'")
                if ":latest" in v:
                    print("FAIL :latest image %s:%d: %s" % (path, i, v))
                    bad = 1
                elif "@sha256:" not in v:
                    print("FAIL image without digest %s:%d: %s" % (path, i, v))
                    bad = 1
print("PASS yaml+digest (%d file(s))" % len(sys.argv[1:]) if not bad else "FAIL yaml+digest")
sys.exit(bad)
PY
}

validate_with_ruby() {
  local files=("$@")
  if ruby -ryaml -e 'ARGV.each { |f| YAML.load_stream(File.read(f)) }' "${files[@]}" 2>/tmp/oa_ruby_err; then
    echo "PASS yaml (ruby fallback, ${#files[@]} file(s))"
  else
    echo "FAIL yaml (ruby fallback):"
    cat /tmp/oa_ruby_err
    status=1
  fi
}

grep_digest_checks() {
  local files=("$@")
  local f bad=0
  for f in "${files[@]}"; do
    if grep -nE '^[[:space:]]*repository:.*:latest' "$f" >/dev/null 2>&1; then
      echo "FAIL :latest repository in $f"
      grep -nE '^[[:space:]]*repository:.*:latest' "$f"
      bad=1
    fi
    if grep -nE '^[[:space:]]*digest:[[:space:]]*("")?[[:space:]]*$' "$f" >/dev/null 2>&1; then
      echo "FAIL empty digest in $f"
      bad=1
    fi
    if grep -nE '^[[:space:]]*digest:' "$f" | grep -vE 'sha256:[0-9a-f]{64}' >/dev/null 2>&1; then
      echo "FAIL malformed digest in $f"
      grep -nE '^[[:space:]]*digest:' "$f" | grep -vE 'sha256:[0-9a-f]{64}'
      bad=1
    fi
  done
  if [ "$bad" -ne 0 ]; then status=1; fi
}

collect_source_files() {
  local f
  [ -d "$VALUES_DIR" ] && for f in "$VALUES_DIR"/*.yaml; do [ -e "$f" ] && echo "$f"; done
  for f in "$HELM_DIR"/*/Chart.yaml "$HELM_DIR"/*/values.yaml; do [ -e "$f" ] && echo "$f"; done
}

# ------------------------------------------------------------------ helm path
run_helm() {
  local tmpdir
  tmpdir="$(mktemp -d)"
  local -a rendered
  rendered=()
  local chart env rel out_a out_b
  for chart in "${CHARTS[@]}"; do
    for env in "${ENVS[@]}"; do
      rel="oa-${chart#ontoaxis-}"
      out_a="$tmpdir/${chart}-${env}-a.yaml"
      out_b="$tmpdir/${chart}-${env}-b.yaml"
      local -a args
      args=("$rel" "$HELM_DIR/$chart")
      [ -f "$VALUES_DIR/common.yaml" ] && args+=(-f "$VALUES_DIR/common.yaml")
      args+=(-f "$VALUES_DIR/$env.yaml")

      if ! helm template "${args[@]}" >"$out_a" 2>"$tmpdir/err"; then
        echo "FAIL helm template $chart x $env"
        cat "$tmpdir/err"
        status=1
        continue
      fi
      helm template "${args[@]}" >"$out_b" 2>>"$tmpdir/err"
      if [ "$(sha "$out_a")" = "$(sha "$out_b")" ]; then
        echo "PASS deterministic render $chart x $env"
      else
        echo "FAIL non-deterministic render $chart x $env"
        status=1
      fi
      rendered+=("$out_a")

      if command -v kubeconform >/dev/null 2>&1; then
        if kubeconform -strict -summary -ignore-missing-schemas "$out_a" >/dev/null 2>"$tmpdir/kcerr"; then
          echo "PASS kubeconform $chart x $env"
        else
          echo "FAIL kubeconform $chart x $env"
          cat "$tmpdir/kcerr"
          status=1
        fi
      fi
    done
  done

  if command -v kubeconform >/dev/null 2>&1; then :; else
    echo "SKIP kubeconform not installed"
  fi

  if python_has_yaml; then
    validate_with_python "${rendered[@]}" || status=1
  else
    echo "SKIP PyYAML not available for rendered-output parse; using ruby/grep"
    validate_with_ruby "${rendered[@]}"
    grep_digest_checks "${rendered[@]}"
  fi
  rm -rf "$tmpdir"
}

# ------------------------------------------------------------------ main flow
echo "OntoAxis delivery render check"
echo "------------------------------"

SOURCE_FILES=()
while IFS= read -r _line; do
  [ -n "$_line" ] && SOURCE_FILES+=("$_line")
done < <(collect_source_files)

if command -v helm >/dev/null 2>&1; then
  echo "helm: $(helm version --short 2>/dev/null || echo present)"
  run_helm
else
  echo "SKIP helm not installed"
  if python_has_yaml; then
    validate_with_python "${SOURCE_FILES[@]}" || status=1
  elif command -v ruby >/dev/null 2>&1; then
    validate_with_ruby "${SOURCE_FILES[@]}"
    grep_digest_checks "${SOURCE_FILES[@]}"
  else
    echo "SKIP no YAML parser available; grep-only fallback"
    grep_digest_checks "${SOURCE_FILES[@]}"
  fi
fi

echo "------------------------------"
if [ "$status" -eq 0 ]; then
  echo "RESULT: PASS"
else
  echo "RESULT: FAIL"
fi
exit "$status"
