# OntoAxis Helm charts

Five lifecycle charts deliver the `production-delivery` change. Each chart is
standalone (no chart-library dependency); shared template logic is copied into
each chart's `templates/_helpers.tpl` so any chart can be rendered on its own.

```
deploy/helm/
├── ontoaxis-platform/      # control-plane data services + all Namespace objects
├── ontoaxis-data/          # streaming / CDC / analytics / catalog engines
├── ontoaxis-app/           # OntoAxis application tier
├── ontoaxis-observability/ # metrics / traces / dashboards
└── ontoaxis-jobs/          # PostgreSQL backup CronJob + PVC
```

## Chart map

| Chart | Namespace | Components (`components:` keys) |
| --- | --- | --- |
| `ontoaxis-platform` | `oa-system` (also creates `oa-app`, `oa-data`, `oa-observability`) | `postgres`, `keycloak`, `openfga`, `valkey`, `seaweedfs` |
| `ontoaxis-data` | `oa-data` | `kafka`, `debezium`, `apicurio`, `kestra`, `temporal`, `trino`, `nessie` |
| `ontoaxis-app` | `oa-app` | `ontoaxis-core`, `web`, `meilisearch`, `ontoaxis-search` (worker, no Service) |
| `ontoaxis-observability` | `oa-observability` | `otel-collector`, `prometheus`, `alertmanager`, `jaeger`, `perses` |
| `ontoaxis-jobs` | `oa-data` | `backup` block → `CronJob` + `PVC` + `ConfigMap` + `ExternalSecret` |

Every component is a key under `.Values.components` and carries a `chart:
<chart-name>` tag; templates render only components whose tag matches the chart
being rendered (`and $c.enabled (eq $c.chart $.Chart.Name)`). This lets a single
environment overlay be applied to every chart: keys belonging to another chart
are ignored.

## Template contract

Each chart ships these templates (driven by `.Values.components`):

| Template | Renders | Guard |
| --- | --- | --- |
| `deployment.yaml` | `Deployment` | `$c.enabled` |
| `service.yaml` | `Service` | `$c.service.enabled` |
| `configmap.yaml` | `ConfigMap` | `$c.configMap.enabled` |
| `serviceaccount.yaml` | `ServiceAccount` | `$c.serviceAccount.create` |
| `pdb.yaml` | `PodDisruptionBudget` | `$c.pdb.enabled` |
| `external-secret.yaml` | `ExternalSecret` (ESO) | `$c.externalSecret.enabled` |
| `namespaces.yaml` | 4 `Namespace`s (`ontoaxis-platform` only) | `.Values.createNamespaces` |

`ontoaxis-jobs` additionally ships `cronjob.yaml`, `pvc.yaml`, `configmap.yaml`
(backup script), `serviceaccount.yaml`, `external-secret.yaml`, and an optional
`manual-job.yaml` (gated by `backup.manualJob.enabled`).

## Labels

Every object gets standard labels from `ontoaxis.labels`:

- `app.kubernetes.io/name` = chart name
- `app.kubernetes.io/instance` = release name
- `app.kubernetes.io/managed-by` = release service (Helm/ArgoCD)
- `app.kubernetes.io/part-of` = `ontoaxis`
- `app.kubernetes.io/version` = chart `appVersion`
- `ontoaxis.io/component` = component key
- `ontoaxis.io/environment` = `global.environment`

Selectors use the stable subset `ontoaxis.selectorLabels`
(`name` + `instance` + `ontoaxis.io/component`).

## Image contract (hard rule)

Images render **only** as `repository@sha256:<64 hex>` via the `ontoaxis.image`
helper. The helper **fails the render** when:

- `image.repository` is missing,
- `image.digest` is missing/empty,
- `image.digest` is not `sha256:` + exactly 64 lowercase hex characters,
- `image.repository` contains `:latest`,
- `image.repository` carries a tag (tag-without-digest form).

There is no tag fallback and no `:latest` anywhere. The placeholder digests in
the shipped values are deterministic (`sha256("<placeholder>:<name>")`) so the
charts render for validation; **they must be replaced by the release manifest**
before any real deployment.

## Required values

Global (see each chart `values.yaml`, overridden by `deploy/values/*.yaml`):

| Key | Required | Meaning |
| --- | --- | --- |
| `global.environment` | yes | one of `kind` \| `kind-small` \| `staging-like` \| `prod-like`; validated, invalid → render fails |
| `global.imagePullPolicy` | no (default `IfNotPresent`) | image pull policy |
| `global.defaultReplicas` | no (default `1`) | fallback replicas |
| `global.defaultResources` | no | fallback requests/limits |
| `global.oidc.issuer` | yes when any component sets `oidcEnv: true` | OIDC issuer URL |
| `global.oidc.audience` | yes when any component sets `oidcEnv: true` | OIDC audience |
| `global.oidc.jwksUrl` | no | optional explicit JWKS URL |
| `global.secretStore.name` | yes when any `ExternalSecret` renders | `ClusterSecretStore` name |
| `global.secretStore.kind` | no (default `ClusterSecretStore`) | secret store kind |

Per component (`.Values.components.<name>`):

| Key | Required | Meaning |
| --- | --- | --- |
| `chart` | yes | owning chart name (render filter) |
| `enabled` | yes (true to render) | render toggle |
| `namespace` | yes | target namespace |
| `image.repository` | yes | registry/repo, **no tag** |
| `image.digest` | yes | `sha256:<64 hex>` |
| `replicas` | no | defaults to `global.defaultReplicas` |
| `ports[]` | yes when a container/Service exists | `{name,containerPort,protocol}` |
| `env[]` | no | `{name,value}` literal env |
| `secretRefs[]` | no | `{name,secretName,key}` — names/keys only; rendered with `required` |
| `oidcEnv` | no | inject `ONTOAXIS_OIDC_*` env from `global.oidc` |
| `resources` | no | defaults to `global.defaultResources` |
| `probes` | recommended | `mode: http|tcp`; HTTP uses `/healthz` + `/readyz`, TCP for datastores |
| `service` | no | `{enabled,type,ports[]}` |
| `configMap` | no | `{enabled,name,mountPath,data{}}` |
| `serviceAccount` | no | `{create,name}` |
| `pdb` | no | `{enabled,minAvailable}` |
| `terminationGracePeriodSeconds` / `preStop` | no | graceful shutdown |
| `strategy` | no | defaults to `RollingUpdate` (app core uses `Recreate`) |
| `externalSecret` | no | `{enabled,secretStoreRef,targetName,refreshInterval,data[{secretKey,remoteRef{key,property,fullKey}}]}` |

`ExternalSecret` remote keys default to `ontoaxis/<environment>/<key>` unless
`remoteRef.fullKey: true`, in which case `remoteRef.key` is used verbatim. Secret
**values are never in values files** — only store/key names.

## Namespaces

`oa-system`, `oa-app`, `oa-data`, `oa-observability` are created **only** by
`ontoaxis-platform` (`createNamespaces: true`, optional ArgoCD sync-wave `-10`).
Application/data/observability/jobs charts never create Namespaces; each
component chooses its namespace via `namespace:` in its values block.

## Backup job (`ontoaxis-jobs`, task 7.1)

- `CronJob` `ontoaxis-backup`, schedule `0 2 * * *` (daily 02:00 UTC),
  `concurrencyPolicy: Forbid`.
- Uses `pg_dump -Fc` into a PVC (`ontoaxis-backups`, default `10Gi`).
- Each run writes `manifest.json` with: `backup_id`, `created_at`, `sha256`,
  `size_bytes`, `schema_version`, `image_digest`, `pg_version`.
- Retention keeps the newest `retention: 7` backup directories (older pruned).
- Manual run: `kubectl create job --from=cronjob/ontoaxis-backup -n oa-data <name>`
  (or set `backup.manualJob.enabled: true` for a declarative one-off Job).
- DB password/user/database come from Secret `ontoaxis-backup-secrets`, projected
  by the chart's `ExternalSecret`; never literal values.

## Rendering

```sh
# one chart + one environment overlay (common.yaml is shared defaults)
helm template oa-platform deploy/helm/ontoaxis-platform \
  -f deploy/values/common.yaml -f deploy/values/kind.yaml

# lint (requires helm 3.x)
helm lint deploy/helm/ontoaxis-platform -f deploy/values/common.yaml -f deploy/values/kind.yaml

# static (no helm needed; prints SKIP for helm steps when absent)
./scripts/delivery-render-check.sh
./scripts/delivery-version-check.sh
```

## Validation status (this workspace)

- `helm lint` / `helm template`: **NOT run** — Helm 3.x is not installed here.
  The charts are authored for Helm 3 syntax; run the render gate on a host with
  Helm before applying.
- Static YAML validation of `Chart.yaml`, `values.yaml` and `deploy/values/*.yaml`:
  **passed** via Ruby YAML (PyYAML is absent in this workspace; the render check
  falls back accordingly and also grep-checks `:latest` / missing digests).
- Component semantic checks (chart tag, digest format, service/configmap/pdb
  consistency, backup schedule/retention/manifest fields): **passed**.
- kind/k3d exercise of the Helm path: **not run** here; Compose remains the local
  run path (see [`deploy/README.md`](../README.md)).
