#!/usr/bin/env bash
# delivery-idp-check.sh — verify OIDC discovery consistency and (optionally) a
# real token against the core read-only API.
#
# Checks:
#   * discovery document issuer == configured issuer;
#   * token_endpoint / jwks_uri present; RS256 advertised;
#   * JWKS has at least one key with a kid;
#   * optional: OIDC_TEST_TOKEN, when set, is used against the core read-only API
#     and must yield HTTP 200 (the token itself is never printed).
#
# IDP_NEGATIVE=audience|issuer|expired|bad_signature|bad_kid|missing_claims
# requires an externally prepared OIDC_TEST_TOKEN and a distinct positive
# OIDC_CONTROL_TOKEN. This checks transport and HTTP rejection, not whether
# the submitted sample was prepared correctly for the named case.
#
# Usage: scripts/delivery-idp-check.sh [--issuer URL] [--core-url URL]
set -euo pipefail

ISSUER="${OIDC_ISSUER:-http://127.0.0.1:18088/realms/ontoaxis}"
CORE_URL="${OIDC_CORE_URL:-http://127.0.0.1:18088}"
READONLY_API_PATH="${OIDC_READONLY_API_PATH:-/api/v1/tenancy/organizations}"
NEGATIVE="${IDP_NEGATIVE:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --issuer) ISSUER="${2:-}"; shift 2 ;;
    --core-url) CORE_URL="${2:-}"; shift 2 ;;
    -h|--help) grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

pass=0; fail=0
ok() { pass=$((pass + 1)); echo "PASS $1"; }
no() { fail=$((fail + 1)); echo "FAIL $1"; }

log() { printf '\033[1;34m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl not found"
TOKEN="${OIDC_TEST_TOKEN:-}"
CONTROL="${OIDC_CONTROL_TOKEN:-}"
if [ -n "$NEGATIVE" ]; then
  case "$NEGATIVE" in audience|issuer|expired|bad_signature|bad_kid|missing_claims) ;; *) die 'unknown IDP_NEGATIVE case' ;; esac
  if [ -z "$TOKEN" ] || [ -z "$CONTROL" ] || [ "$TOKEN" = "$CONTROL" ]; then
    die 'negative check requires distinct OIDC_TEST_TOKEN and OIDC_CONTROL_TOKEN'
  fi
fi

# Network input is explicit; never send a bearer along a redirect or through
# plain HTTP to a non-loopback address. Keep token material out of process args.
fetch() { curl --disable --silent --show-error --fail --max-time 8 --noproxy 'localhost,127.0.0.1,::1' "$@"; }
log 'fetching configured OIDC discovery document'
disc="$(fetch -- "${ISSUER}/.well-known/openid-configuration" 2>/dev/null)" || die "cannot fetch OIDC discovery document"

issuer_out="$(python3 - "$disc" "$ISSUER" <<'PY'
import json, sys
from urllib.parse import urlsplit
raw, expected = sys.argv[1:3]
try:
    doc = json.loads(raw)
except Exception as e:
    print(f"ERR invalid JSON: {e}"); sys.exit(0)
problems = []
if doc.get("issuer") != expected:
    problems.append("discovery issuer differs from configured issuer")
origin = urlsplit(expected)
for field in ("token_endpoint", "jwks_uri", "authorization_endpoint"):
    value = doc.get(field)
    endpoint = urlsplit(value) if isinstance(value, str) else None
    if not endpoint or (endpoint.scheme, endpoint.netloc) != (origin.scheme, origin.netloc):
        problems.append(f"missing or unexpected origin for {field}")
algs = doc.get("id_token_signing_alg_values_supported") or []
if "RS256" not in algs:
    problems.append(f"RS256 not advertised (got {algs})")
jwks = doc.get("jwks_uri", "")
print("\n".join(problems) if problems else "OK")
print("JWKS_URI=" + jwks)
PY
)"
problems="$(printf '%s\n' "$issuer_out" | sed -n '1,/^JWKS_URI=/p' | grep -v '^JWKS_URI=' | grep -v '^OK$' || true)"
jwks_uri="$(printf '%s\n' "$issuer_out" | sed -n 's/^JWKS_URI=//p')"
if [ -n "$problems" ] || [ -z "$jwks_uri" ]; then die 'OIDC discovery issuer, endpoint or signing algorithm is invalid'; fi
ok 'discovery issuer/endpoints/alg consistent'

if [ -n "$jwks_uri" ]; then
  log 'fetching configured JWKS'
  if fetch -- "$jwks_uri" 2>/dev/null | python3 -c '
import json,sys
doc=json.load(sys.stdin)
keys=doc.get("keys") or []
if not keys: sys.exit("no keys")
if not any(k.get("kid") for k in keys): sys.exit("no kid")
print("JWKS keys:", len(keys))
' >/dev/null 2>&1; then
    ok "JWKS has keys with kid"
  else
    no "JWKS missing keys or kid"
  fi
else
  no "no jwks_uri to validate"
fi

if [ -n "$TOKEN" ]; then
  # Reuse bounded, redirect-free transport and enforce HTTPS for non-local JWTs.
  codes="$(PYTHONDONTWRITEBYTECODE=1 python3 - "${BASH_SOURCE[0]}" "$CORE_URL" "$READONLY_API_PATH" <<'PY'
import importlib.util
import os
from pathlib import Path
import sys
spec = importlib.util.spec_from_file_location("delivery_smoke", Path(sys.argv[1]).resolve().with_name("delivery-smoke.py"))
lib = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lib)
base, path = sys.argv[2:]
try:
    lib.require(path.startswith("/") and not path.startswith("//"), "invalid API path")
    endpoint = base.rstrip("/") + path
    if os.getenv("IDP_NEGATIVE"):
        status, _ = lib.request(endpoint, os.environ["OIDC_CONTROL_TOKEN"])
        print(f"{status:03d}")
        if status != 200:
            sys.exit(0)
    status, _ = lib.request(endpoint, os.environ["OIDC_TEST_TOKEN"])
    print(f"{status:03d}")
except (lib.CheckFailed, ValueError):
    sys.exit("invalid token or API endpoint")
PY
)" || die 'could not validate live API request'
  code="${codes##*$'\n'}"
  if [ -n "$NEGATIVE" ]; then
    if [[ "$codes" != 200$'\n'* ]]; then die "positive control did not return HTTP 200 (got ${code})"; fi
    case "$code" in
      401|403) ok "candidate submitted for '${NEGATIVE}' rejected (HTTP ${code}); verify fixture semantics separately" ;;
      *) no "candidate submitted for '${NEGATIVE}' expected 401/403, got HTTP ${code}" ;;
    esac
  else
    if [ "$code" = "200" ]; then ok "test token accepted by core read-only API (HTTP 200)"; else no "test token not accepted (HTTP ${code})"; fi
  fi
else
  log 'discovery/JWKS only; live-token API check not executed'
fi

echo "idp-check: ${pass} passed, ${fail} failed"
[ "$fail" -eq 0 ]
