# ArgoCD GitOps layout for OntoAxis

App-of-apps GitOps for the `production-delivery` change. **Nothing here has been
run against a live ArgoCD in Stage 2** — these are reviewed manifests; evidence
is marked `not executed` where applicable.

## Layout

```
deploy/argocd/
  projects/ontoaxis.yaml          AppProject (restricted sourceRepos + destinations)
  root-app.yaml                   root Application for kind/staging-like (automated)
  root-app.prod-like.yaml         root Application for prod-like (MANUAL sync)
  apps/                           child Applications for kind (auto prune+selfHeal)
  apps-prod-like/                 child Applications for prod-like (manual)
  manifests/namespaces/           the four oa-* Namespaces (sync-wave -10)
  secrets/kind/                   ClusterSecretStore + ExternalSecrets (kubernetes provider)
  secrets/staging-like/           same, staged store name
  secrets/prod-like/              vault template (NOT connected)
  secrets/README.md               key inventory + owner/rotation
```

The edge is an **ArgoCD Directory app over `deploy/edge/generated/`** — the
generated Envoy `HTTPRoute`/`Gateway`/policies are the single source of truth and
are not copied. Envoy Gateway itself is installed by `deploy/kind/bootstrap.sh`
via `deploy/edge/kind/install-envoy-gateway.sh`.

## Namespaces and sync waves

| Wave | Purpose | Contents |
| --- | --- | --- |
| `-10` | namespaces | `oa-system`, `oa-app`, `oa-data`, `oa-observability` |
| `-5` | secrets | `ClusterSecretStore` + `ExternalSecret` (ESO materialises target Secrets) |
| `0` | platform | `ontoaxis-platform` (Envoy Gateway glue, Keycloak, OpenFGA) |
| `1` | data | `ontoaxis-data` (PostgreSQL, Kafka, Debezium, Trino, Nessie, ...) |
| `2` | app | `ontoaxis-app` (core, web, Meilisearch, Valkey, SeaweedFS) — core migrates at startup |
| `3` | edge | generated Envoy `HTTPRoute`/`Gateway` |
| `4` | observability | OTel, Prometheus, Alertmanager, Jaeger, Perses |
| `5` | jobs | one-shot/migration jobs (`PruneLast`, applied after app is Healthy) |

Each child `Application` carries `argocd.argoproj.io/sync-wave` in its
annotations. Orders within a wave are handled by ArgoCD; the wave order enforces
that secrets exist before workloads and that data is healthy before the app.

## Automated vs manual sync

- **kind / staging-like** — `root-app.yaml`: `syncPolicy.automated` with
  `prune: true` and `selfHeal: true`. Child apps also use automated sync with a
  bounded retry.
- **prod-like** — `root-app.prod-like.yaml` and `apps-prod-like/applications.yaml`
  have **no** `automated` block. Production changes are synced manually and must
  be reviewed; jobs still use `PruneLast`.

Manual prod sync (once a prod-like target exists):

```sh
argocd app sync ontoaxis-root --grpc-web   # or sync individual child apps in wave order
argocd app wait ontoaxis-root --health --grpc-web
```

## Applying by hand (kind)

```sh
kubectl apply -f deploy/argocd/projects/ontoaxis.yaml
kubectl apply -f deploy/argocd/root-app.yaml
```

`deploy/kind/bootstrap.sh` does this for you after installing ArgoCD, ESO, Envoy
Gateway and the bootstrap Secret.

## Placeholders to replace

- `repoURL: https://github.com/ontoaxis/ontoaxis.git` and `targetRevision: main`
  are placeholders — point them at the real GitOps repository/branch.
- Release digests in `deploy/releases/*.yaml` start as all-zero placeholders;
  `scripts/delivery-release-check.sh` fails until they are replaced.
