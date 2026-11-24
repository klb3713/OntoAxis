#!/usr/bin/env bash
# Retrieval-quality baseline for object search (`object-search-meilisearch` 8.3).
#
# Loads the synthetic Chinese fixture in fixtures/search/dataset.json into a
# throwaway Meilisearch index, runs the fixture queries, and reports recall@5
# and MRR@10.
#
# Two separate gates, do not conflate:
#   * engineering regression gate (`engineering_gate`, signed): the frozen
#     synthetic corpus must not regress below the recorded baseline. --strict
#     enforces this gate in CI.
#   * business quality thresholds (`thresholds`): still pending-owner-signoff and
#     reported for information only; never use them to declare business
#     acceptance.
#
# Usage:
#   MEILI_URL=http://127.0.0.1:7700 MEILI_API_KEY=... scripts/search-quality-baseline.sh
#   scripts/search-quality-baseline.sh --strict   # exit 3 when the signed
#                                                 # engineering gate fails
#
# Exit codes: 0 measured (no --strict), 2 infrastructure/setup failure,
# 3 engineering regression gate failed (metrics below baseline, corpus drift,
# or gate unsigned) in --strict mode.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATASET="${ROOT_DIR}/fixtures/search/dataset.json"
MEILI_URL="${MEILI_URL:-${ONTOAXIS_SEARCH_TEST_MEILISEARCH_URL:-http://127.0.0.1:7700}}"
MEILI_API_KEY="${MEILI_API_KEY:-${ONTOAXIS_SEARCH_TEST_API_KEY:-}}"
STRICT=0
[ "${1:-}" = "--strict" ] && STRICT=1

if [ ! -f "${DATASET}" ]; then
  echo "dataset not found: ${DATASET}" >&2
  exit 2
fi
if ! curl -fsS -o /dev/null "${MEILI_URL}/health"; then
  echo "meilisearch not reachable at ${MEILI_URL}" >&2
  exit 2
fi

tmp="$(mktemp -d)"
uid="search_quality_baseline_$(date +%s)"
cleanup() {
  local code=$?
  curl -sS -o /dev/null -X DELETE "${MEILI_URL}/indexes/${uid}" \
    -H "Authorization: Bearer ${MEILI_API_KEY}" || true
  rm -rf "${tmp}"
  exit "${code}"
}
trap cleanup EXIT

auth=(-H "Authorization: Bearer ${MEILI_API_KEY}")

wait_task() {
  local task_uid="$1" deadline=$((SECONDS + 120)) status
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    status=$(curl -sS "${MEILI_URL}/tasks/${task_uid}" "${auth[@]}" |
      python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')
    case "${status}" in
      succeeded) return 0 ;;
      failed | canceled) echo "meilisearch task ${task_uid} ${status}" >&2; return 1 ;;
    esac
    sleep 0.2
  done
  echo "meilisearch task ${task_uid} timed out" >&2
  return 1
}

python3 - "${DATASET}" "${tmp}" <<'PY'
import json, sys
dataset = json.load(open(sys.argv[1], encoding="utf-8"))
out = sys.argv[2]
json.dump(dataset["documents"], open(out + "/documents.json", "w", encoding="utf-8"), ensure_ascii=False)
json.dump(dataset["queries"], open(out + "/queries.json", "w", encoding="utf-8"), ensure_ascii=False)
settings = {
    "searchableAttributes": ["display_name", "properties_text"],
    "filterableAttributes": ["tenant_id", "object_type_id", "updated_at"],
    "sortableAttributes": ["updated_at", "created_at"],
    "displayedAttributes": ["id", "object_type_id", "display_name", "version", "updated_at"],
    "pagination": {"maxTotalHits": 10000},
}
json.dump(settings, open(out + "/settings.json", "w", encoding="utf-8"), ensure_ascii=False)
PY

echo "== creating index ${uid} =="
task=$(curl -sS -X POST "${MEILI_URL}/indexes" "${auth[@]}" -H 'Content-Type: application/json' \
  -d "{\"uid\":\"${uid}\",\"primaryKey\":\"id\"}" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["taskUid"])')
wait_task "${task}"

task=$(curl -sS -X PATCH "${MEILI_URL}/indexes/${uid}/settings" "${auth[@]}" \
  -H 'Content-Type: application/json' --data-binary @"${tmp}/settings.json" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["taskUid"])')
wait_task "${task}"

task=$(curl -sS -X POST "${MEILI_URL}/indexes/${uid}/documents" "${auth[@]}" \
  -H 'Content-Type: application/json' --data-binary @"${tmp}/documents.json" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["taskUid"])')
wait_task "${task}"
deadline=$((SECONDS + 120))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  indexed=$(curl -sS "${MEILI_URL}/indexes/${uid}/stats" "${auth[@]}" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["numberOfDocuments"])' 2>/dev/null || echo 0)
  [ "${indexed}" = "24" ] && break
  sleep 0.3
done

echo "== measuring =="
python3 - "${tmp}/queries.json" "${MEILI_URL}" "${uid}" "${MEILI_API_KEY}" "${tmp}/results.json" <<'PY'
import json, sys, urllib.request

queries = json.load(open(sys.argv[1], encoding="utf-8"))
base, uid, key, out = sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]

rows = []
for query in queries:
    req = urllib.request.Request(
        f"{base}/indexes/{uid}/search",
        data=json.dumps({"q": query["q"], "limit": 10}).encode(),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {key}"},
        method="POST",
    )
    with urllib.request.urlopen(req) as resp:
        hits = json.load(resp).get("hits", [])
    ids = [hit["id"] for hit in hits]
    relevant = set(query["relevant"])
    found5 = relevant.intersection(ids[:5])
    recall5 = len(found5) / len(relevant) if relevant else 0.0
    rank = next((i + 1 for i, doc_id in enumerate(ids) if doc_id in relevant), None)
    rr = 1.0 / rank if rank else 0.0
    rows.append({"id": query["id"], "q": query["q"], "hits": ids,
                 "relevant": sorted(relevant), "recall_at_5": recall5, "rr": rr})
    print(f"{query['id']}  q={query['q']!r}  recall@5={recall5:.2f}  rr={rr:.2f}  hits={ids}")

recall = sum(row["recall_at_5"] for row in rows) / len(rows)
mrr = sum(row["rr"] for row in rows) / len(rows)
json.dump({"recall_at_5": recall, "mrr_at_10": mrr, "queries": rows},
          open(out, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print(f"\nrecall@5={recall:.4f}  MRR@10={mrr:.4f}")
PY

python3 - "${DATASET}" "${tmp}/results.json" <<'PY' > "${tmp}/verdict"
import json, sys, hashlib

dataset = json.load(open(sys.argv[1], encoding="utf-8"))
metrics = json.load(open(sys.argv[2], encoding="utf-8"))

# Business thresholds: informational only, unsigned.
th = dataset.get("thresholds", {})
print(f"business thresholds (UNSIGNED): recall@5>={th.get('recall_at_5')} "
      f"mrr@10>={th.get('mrr_at_10')} status={th.get('status')} owner={th.get('owner')}")

# Engineering regression gate: frozen corpus must not regress below baseline.
eg = dataset.get("engineering_gate") or {}
if eg.get("status") != "signed-off":
    print("engineering gate: UNSIGNED (no signed engineering_gate block)")
    print("UNSIGNED")
    sys.exit(0)

baseline = eg.get("baseline", {})
base_recall = float(baseline.get("recall_at_5", 1.0))
base_mrr = float(baseline.get("mrr_at_10", 1.0))
recall_ok = round(metrics["recall_at_5"], 4) + 1e-9 >= base_recall
mrr_ok = round(metrics["mrr_at_10"], 4) + 1e-9 >= base_mrr
corpus = json.dumps({"documents": dataset["documents"], "queries": dataset["queries"]},
                    ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
corpus_sha = hashlib.sha256(corpus).hexdigest()
drift = corpus_sha != eg.get("corpus_sha256")
print(f"engineering gate: baseline recall@5={base_recall} mrr@10={base_mrr} "
      f"owner={eg.get('owner')} signed_at={eg.get('signed_at')}")
print(f"engineering comparison: recall@5={'meets' if recall_ok else 'below-baseline'} "
      f"mrr@10={'meets' if mrr_ok else 'below-baseline'} corpus_sha256_match={not drift}")
print("NOTICE: business thresholds are unsigned; this run is a technical baseline, not a business acceptance.")
print("PASS" if (recall_ok and mrr_ok and not drift) else "FAIL")
PY

cat "${tmp}/verdict"
verdict=$(tail -1 "${tmp}/verdict")

if [ "${STRICT}" = "1" ] && [ "${verdict}" != "PASS" ]; then
  echo "strict mode: engineering regression gate failed (exit 3; acceptance=${verdict}); business thresholds remain unsigned" >&2
  exit 3
fi
exit 0