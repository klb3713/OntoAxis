# Secrets: key inventory, ownership and rotation

This directory holds the **references** to secrets — never the values. ArgoCD
syncs `ExternalSecret` objects; the External Secrets Operator (ESO) fetches the
values from the backing provider and materialises Kubernetes `Secret` objects
that the workloads consume through `secretKeyRef` / `valueFrom` only.

| Path | Provider | Values source |
| --- | --- | --- |
| `kind/` | ESO `kubernetes` provider | bootstrap `Secret` `ontoaxis-bootstrap` in ns `external-secrets`, created by `scripts/delivery-secrets-bootstrap.sh` from `.env.kind` |
| `staging-like/` | ESO `kubernetes` provider | same bootstrap `Secret`, populated from environment variables or a git-ignored `.env` file |
| `prod-like/` | ESO `vault` provider — **template, not connected** | logical path `ontoaxis/prod-like/<component>/<key>`; `server: https://vault.invalid` fails closed if ever synced |

`kind` 与 `kind-small` 共用 wave -5 的 `kind/` 清单。它是这些目标
Secret 的唯一 ESO 所有者；环境 overlay 禁用各 chart 的同名目标生成，
工作负载保留 `secretKeyRef`。单独使用 `helm template` 不会生成这些 Secret，
必须先由受控 GitOps 安装 SecretStore 和 ExternalSecret。暂不改变其他环境的供给边界。

## `ontoaxis-bootstrap` Secret keys

The `kubernetes` provider can only reference **valid Secret key names**, so the
remote keys are flat (no `/`). Logical groupings for an external provider are
shown for reference only.

| Bootstrap key (flat) | Logical path (external) | Purpose |
| --- | --- | --- |
| `database-url` | `ontoaxis/<env>/core/database-url` | Core DSN (contains DB password) |
| `oidc-client-secret` | `ontoaxis/<env>/core/oidc-client-secret` | Keycloak OIDC client secret for core/web |
| `meilisearch-master-key` | `ontoaxis/<env>/app/meilisearch-master-key` | Meilisearch master key |
| `keycloak-bootstrap-admin-username` | `ontoaxis/<env>/keycloak/bootstrap-admin-username` | Keycloak bootstrap admin user |
| `keycloak-bootstrap-admin-password` | `ontoaxis/<env>/keycloak/bootstrap-admin-password` | Keycloak bootstrap admin password |
| `keycloak-db-password` | `ontoaxis/<env>/keycloak/db-password` | Keycloak database password |
| `postgres-password` | `ontoaxis/<env>/data/postgres-password` | PostgreSQL superuser/user password |
| `trino-user` | `ontoaxis/<env>/data/trino-user` | Trino basic-auth user |
| `trino-password` | `ontoaxis/<env>/data/trino-password` | Trino basic-auth password |
| `seaweedfs-s3-access-key` | `ontoaxis/<env>/data/seaweedfs-s3-access-key` | SeaweedFS/S3 access key |
| `seaweedfs-s3-secret-key` | `ontoaxis/<env>/data/seaweedfs-s3-secret-key` | SeaweedFS/S3 secret key |
| `openfga-token` | `ontoaxis/<env>/openfga/token` | OpenFGA pre-shared token |
| `backup-passphrase` | `ontoaxis/<env>/backup/passphrase` | Backup encryption passphrase |
| `trino-credentials` | `ontoaxis/<env>/data/trino-credentials` | Trino basic-auth composite (`user:password`) |
| `openfga-datastore-uri` | `ontoaxis/<env>/openfga/datastore-uri` | OpenFGA datastore URI |
| `postgres-database` | `ontoaxis/<env>/data/postgres-database` | Backup target database name |
| `postgres-user` | `ontoaxis/<env>/data/postgres-user` | Backup target database user |


### Target Secrets materialised by ESO

| ExternalSecret | Namespace | Secret name |
| --- | --- | --- |
| `ontoaxis-core` | `oa-app` | `ontoaxis-core-secrets` |
| `ontoaxis-keycloak` | `oa-system` | `ontoaxis-keycloak-secrets` |
| `ontoaxis-data` | `oa-data` | `ontoaxis-data-secrets` |
| `ontoaxis-seaweedfs` | `oa-app` | `ontoaxis-data-secrets`（仅 S3 键；`kind`/`kind-small`） |
| `ontoaxis-openfga` | `oa-system` | `ontoaxis-openfga-secrets` |
| `ontoaxis-backup` | `oa-data` | `ontoaxis-backup-secrets` |

## Key inventory summary (what must be provisioned)

- Database password (PostgreSQL) — and the Core DSN that embeds it.
- Keycloak bootstrap admin username/password.
- Keycloak OIDC client secret (core/web client).
- OpenFGA pre-shared token.
- Meilisearch master key.
- Trino credentials (user + password).
- SeaweedFS / S3 access key + secret key.
- Backup passphrase.

## Ownership and rotation

- **Owner:** platform / delivery operator (the person running the delivery
  scripts for that environment). `/owner` recorded in the change evidence, not
  in Git values.
- **Storage of truth:** the backing provider / cluster Secret, never this repo.
  `kind` and `staging-like` use the git-ignored `.env.kind` / environment; the
  file is not committed (`.gitignore` `.env.*` rule).
- **Rotation:** rotate in the backing provider (or re-run
  `scripts/delivery-secrets-bootstrap.sh` with an updated `.env`), then let ESO
  refresh (default `refreshInterval: 1h`) or force with
  `kubectl annotate externalsecret <name> -n <ns> force-sync=$(date +%s) --overwrite`.
  Restart pods that cache credentials at startup (`kubectl rollout restart`).
- **Verification:** `kubectl get externalsecret -A` should show
  `STATUS=SecretSynced`; `kubectl get secret <name> -n <ns>` must exist. Never
  echo values (`delivery-secrets-bootstrap.sh` and `delivery-secret-scan.sh`
  enforce this).
- **prod-like:** values are provisioned in the real external provider out of
  band; this repo only carries the reference shape. Wiring a real provider is a
  later, separately-authorised step — the manifest is intentionally not
  connected here.
