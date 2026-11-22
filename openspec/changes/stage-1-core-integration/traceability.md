# Traceability

| Stage AC | Evidence | Status |
| --- | --- | --- |
| S1-AC-01 | `go build/test`, `internal/app/modules_test.go`, `/readyz` | pass |
| S1-AC-02 | Keycloak OIDC, `internal/platform/authn`, 401/403/trusted-header tests, `httpserver/auth_integration_test.go` | pass |
| S1-AC-03 | `internal/modules/tenancy`, API smoke | pass |
| S1-AC-04 | `internal/modules/ontology`, 16 routes | pass |
| S1-AC-05 | `internal/modules/objects`, API smoke | pass |
| S1-AC-06 | `internal/modules/datasets`, PostgreSQL schema | pass |
| S1-AC-07 | `internal/modules/query`, Trino `SELECT 42` and TPCH query | pass |
| S1-AC-08 | `internal/modules/connectors`, `ingestion` tests | pass |
| S1-AC-09 | `internal/modules/pipelines`, `actions` tests | pass (Stage 1 scope) |
| S1-AC-10 | Playwright E2E screenshots `/tmp/ontoaxis-e2e-*.png` | pass |
| S1-AC-11 | Compose, buf checks, CI workflow, local actionlint/shellcheck | partial — GitHub runner and license gate pending |
| S1-AC-12 | Docker images, `config/edge/routes.yaml`, `deploy/README.md` deterministic image id | pass |

## Known gaps

- GitHub Actions has not run on a clean runner.
- `license-check` is implemented in Makefile/CI but the workflow has not run on a GitHub runner.
- `oa-edge` is represented by route configuration + Compose trust mode; production Envoy Gateway manifests remain for Stage 2.
- Stage 1 exit report is a draft pending owner confirmation.
