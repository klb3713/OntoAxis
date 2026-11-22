# Design

## Module assembly

`app.New` receives a `ModuleFactory`. It opens the shared pgx pool, runs platform migrations, then asks the factory for the nine modules. `BuildModules` is the only place that knows concrete module constructors. The registry resolves dependencies and `Migrate` runs in order.

Order: `tenancy -> connectors -> ingestion -> datasets -> pipelines -> ontology -> objects -> query -> actions`.

Cross-module calls use narrow service interfaces:
- `query` receives `objects.Service` for object queries.
- `pipelines` receives a tenant resolver and dataset lookup adapter backed by `datasets.Service`.
- No module imports another module's `repo` package.

## Authentication boundary

The router groups all module routes behind `httpserver.Options.Authenticate`. `authn.EdgeAuthenticator` accepts a JWT or, when `TrustGateway` is enabled for a private compose network, trusted gateway headers. After authentication the app injects:
- `X-Ontoaxis-Tenant-Id` and `X-Ontoaxis-User-Id` into the request;
- authn context, tenancy context, ontology tenant context.

`/healthz`, `/readyz` and `/metrics` are not module routes and remain outside this boundary.

## Local Compose loop

The base compose stack is `postgres + ontoaxis-core`. Host ports are configurable and default to non-conflicting values (`15433`, `18080`, `19090`). One command starts the loop and the smoke script exercises health, readiness, metrics and 404 behavior.

Trino and the web app are additive; they must not be required for the core API smoke.

## Risks

- In-memory adapters (Kestra, action adapters) still simulate external systems.
- Keycloak/JWKS and Trino are not yet part of the minimal loop.
- Generated code and frontend e2e need CI evidence, not only local runs.
