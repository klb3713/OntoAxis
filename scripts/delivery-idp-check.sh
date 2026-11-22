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
# Negative cases are supported as documented env flags (IDP_NEGATIVE):
#   audience | issuer | expired | bad_kid | missing_claims
# When IDP_NEGATIVE is set and a token is provided, the API call is expected to
# be REJECTED (401/403) for that negative case.
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

log "discovery: ${ISSUER}/.well-known/openid-configuration"
disc="$(curl -fsS "${ISSUER}/.well-known/openid-configuration" 2>/dev/null)" || die "cannot fetch OIDC discovery document"

issuer_out="$(python3 - "$disc" "$ISSUER" <<'PY'
import json, sys
raw, expected = sys.argv[1:3]
try:
    doc = json.loads(raw)
except Exception as e:
    print(f"ERR invalid JSON: {e}"); sys.exit(0)
problems = []
if doc.get("issuer") != expected:
    problems.append(f"issuer mismatch: discovery='{doc.get('issuer')}' expected='{expected}'")
for field in ("token_endpoint", "jwks_uri", "authorization_endpoint"):
    if not doc.get(field):
        problems.append(f"missing {field}")
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
if [ -z "$problems" ]; then ok "discovery issuer/endpoints/alg consistent"; else no "discovery checks: $problems"; fi

if [ -n "$jwks_uri" ]; then
  log "jwks: ${jwks_uri}"
  if curl -fsS "$jwks_uri" 2>/dev/null | python3 -c '
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

TOKEN="${OIDC_TEST_TOKEN:-}"
if [ -n "$TOKEN" ]; then
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "${CORE_URL}${READONLY_API_PATH}" 2>/dev/null)" || code=000
  if [ -n "$NEGATIVE" ]; then
    case "$code" in
      401|403) ok "negative case '${NEGATIVE}' correctly rejected (HTTP ${code})" ;;
      *) no "negative case '${NEGATIVE}' expected 401/403, got HTTP ${code}" ;;
    esac
  else
    if [ "$code" = "200" ]; then ok "test token accepted by core read-only API (HTTP 200)"; else no "test token rejected (HTTP ${code})"; fi
  fi
else
  log "no OIDC_TEST_TOKEN set; skipping live-token API check"
  echo "INFO: set OIDC_TEST_TOKEN (not printed) to exercise the read-only API"
  echo "INFO: negative cases via IDP_NEGATIVE=audience|issuer|expired|bad_kid|missing_claims"
fi

echo "idp-check: ${pass} passed, ${fail} failed"
[ "$fail" -eq 0 ]
