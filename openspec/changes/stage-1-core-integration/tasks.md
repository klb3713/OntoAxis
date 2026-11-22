# Tasks

## 1. Module assembly

- [x] 1.1 Replace ad-hoc module construction with `app.ModuleFactory`.
- [x] 1.2 Add `internal/app/modules.go` with the nine-module ordered factory.
- [x] 1.3 Add service accessors for `objects` and `datasets`.
- [x] 1.4 Inject `objects.Service` into `query` and dataset lookup into `pipelines`.

## 2. Authentication and routing

- [x] 2.1 Add `httpserver.Options.Authenticate` around module routes.
- [x] 2.2 Inject trusted gateway headers into authn/tenancy/ontology contexts.
- [x] 2.3 Keep health, readiness and metrics public.
- [x] 2.4 Add an integration test covering unauthenticated rejection and trusted-header acceptance.

## 3. Local loop

- [x] 3.1 Compose the core and PostgreSQL services.
- [x] 3.2 Verify platform and module migrations on a real PostgreSQL.
- [x] 3.3 Add and run the foundation smoke script.
- [x] 3.4 Add a Compose service for a real single-node Trino and a query smoke.
- [x] 3.5 Add a Compose service for the web app and a browser E2E against core.

## 4. Quality gates

- [x] 4.1 `go build ./...` and `go test ./...` pass.
- [x] 4.2 `go vet ./...`, race tests and golangci-lint pass.
- [x] 4.3 `buf lint`, `buf build`, `make proto-breaking` and `make generate-check` pass.
- [x] 4.4 Run CI workflow locally (actionlint/shellcheck/yaml validation) and record evidence.
- [x] 4.5 Verify deterministic image build and a documented rollback procedure.

## 5. Stage 1 evidence

- [x] 5.1 Record module list, readiness output, API smoke results and OpenSpec validation.
- [x] 5.2 Record Trino/web E2E and Keycloak/JWKS evidence.
- [x] 5.3 Produce Stage 1 exit report and traceability matrix.
