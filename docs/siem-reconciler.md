# siem-reconciler

`siem-reconciler` makes the API-only state of a KubeAid SIEM stack (Keycloak,
DFIR-IRIS, Wazuh, Velociraptor, MISP) match one input file, `tenants.json`, rendered by
the `security-operations` Helm chart. It replaces the one-off scripts that used to
set this up by hand, and it is safe to run repeatedly: a second run against an
unchanged system prints only `ok` lines and `0 changes`.

The chart runs it as an ArgoCD PostSync Job and as a CronJob. It can also be run
from a workstation against a cluster (see [Running it by hand](#running-it-by-hand)).
kubeaid-cli renders the chart's values from `cluster.securityOperations`, see
[security-operations.md](security-operations.md).

## What it manages

Components run in this order; `--only` selects a subset.

| Component | Objects | Rule |
|---|---|---|
| `secrets` | Secrets listed under `secrets` | Created with random (or literal `value`) values when missing; missing keys added; existing values never changed. A Secret owned by a SealedSecret is only checked (a missing key is an error, fix the SealedSecret). |
| | Keys listed under `secretCopies` | Target key created or overwritten when its bytes differ from the source; other target keys kept. A missing source is an error for that copy. |
| `enrolment` | One agent enrolment bundle Secret per `enrolment` entry | Rendered from the config and the tenant's authd password; created when missing, updated when any key differs. A missing authd Secret is an error for that bundle. Keys: `manager_host`, `registration_port`, `events_port`, `authd.pass`, `install-linux.sh`, `install-macos.sh`, `install-windows.ps1`; the `velociraptor` step adds two more (below). |
| `keycloak` | Login (kind `login`) | As the `clientCredentials` client when configured, else, or while that client cannot log in yet, as the master-realm admin. See [Keycloak identity](#keycloak-identity). |
| | Realm (created if missing), `bruteForceProtected`, OTP policy, `CONFIGURE_TOTP` as default required action | Only the attributes set in the config are compared. |
| | Realm roles `operators.adminRole`, `operators.analystRole`, group `operators.analystGroup` | Created if missing. |
| | Per tenant: realm role and group `<tenantGroupPrefix><code>`, group grants the role | Created if missing; other role mappings kept. |
| | Clients: settings, secret, protocol mappers, default scopes, scope mappings, service-account client roles | Update on drift for managed fields only (see below). |
| | `browser-mfa` flow | Copy `browser`, OTP sub-flow REQUIRED, its conditions DISABLED, OTP Form REQUIRED, Username Password Form first; re-read and verify; only a verified flow is bound as the realm browser flow. |
| | Optional per-tenant identity-provider broker | Created/updated; a hardcoded-group mapper puts brokered users in the tenant group. |
| `iris` | One customer per tenant (name = tenant name) | Created if missing. |
| | Groups listed in `components.iris.groups` | Created if missing; permission mask reset on drift. |
| | Service accounts listed in `components.iris.serviceAccounts` | Groups added, never removed. Customers: all tenants + the initial customer, added and never removed; or, with `customers`, exactly those (others removed), e.g. one tenant's customer for that tenant's Wazuh manager. With `create: true` a missing login is added as an IRIS service account (no password). With `apiKeySecretRef` the account's API key is kept in that Secret key: a stored key IRIS still accepts as that account (`/user/whoami`) is left alone, otherwise the key is renewed and stored (so a shared key copied there earlier is replaced). |
| `wazuh` | Per tenant manager (reported as `wazuh/<code>`): rules `oidc_<adminRole>`, `oidc_<analystRole>` and `oidc_<g>` (dashes as underscores) mapping the backend roles to `adminApiRole`, `analystApiRole` and `tenantApiRole` | Created, condition corrected, linked to the existing API role. The whole manager belongs to the tenant: no per-tenant policies, roles or agent groups. |
| `wazuhcentral` | Persistent `cluster.remote.<alias>.seeds` on the central indexer | Created or corrected; remotes not in the config are reported as `skip` and left in place. |
| | `dashboardConfigSecret` (`wazuh.yml`) | Rendered from every manager's URL and API credentials; created or updated when it differs. Not written while any manager's credentials are unreadable. |
| | `indexPatterns` on the central dashboard (`dashboardURL`), e.g. `*:wazuh-alerts-*` for cross-cluster search | Created (server-chosen id) when no saved index pattern has that exact title; existing patterns are never modified. Reported as kind `index-pattern`. |
| | The dashboard's `defaultIndex` (kind `default-index`) | Set to the pattern marked `default` only when unset or pointing to a pattern that no longer exists; a valid existing default is left alone. |
| `retention` | Per indexer (reported as `retention/<code>` and `retention/central`): the ISM policy `retention.policyId` | Created or updated when its spec changed: hot until the tenant's `retentionDays` (`retention.centralDays` centrally), then delete; `warmAfterDays` inserts a read-only warm state. The policy's `ism_template` attaches it to new indices. |
| | Existing indices matching `retention.indexPatterns` | Indices with no policy are put under it; indices already under it but on an older policy version are moved to the current one (`change_policy`). Indices under another policy are counted in the `ok` line and left alone. Needs `components.wazuh[].indexer` (and `components.wazuhCentral` for the central one). |
| `content` | Per tenant manager (reported as `content/<code>`): the tenant's rules, decoders and CDB lists from the kubesoc-content package (`components.content.dir`; shared files less `manifest.json` excludes, plus `tenants/<code>/wazuh/` overlays) | Compared with the manager's `etc/` copy; changed files uploaded (`PUT /{rules,decoders,lists}/files/{name}?overwrite=true`), files the previous content owned and this one does not deleted. Then `GET /manager/configuration/validation` and `PUT /cluster/analysisd/reload` (hot reload, no restart); a failed write, an invalid configuration or a reload warning the old content did not have puts every touched file back to the last good content (kept gzipped in Secret `<statePrefix>wazuh-<code>`, label `kubesoc.io/content-version`) and reloads again. The `canary` manager (default the first) goes first; when it fails, the others are reported `skip` and left alone. A content list missing from the manager's `<ruleset>` is an error (register it with `wazuh.ruleset.extraLists`, which restarts the manager). |
| | Agent group `agent.conf` files (`wazuh/agent-groups/<group>/agent.conf`) | Group created if missing; `agent.conf` written when it differs (ignoring the API's re-indentation). Groups are never deleted. |
| | Velociraptor artifacts (with `content.velociraptor`, reported as `content/velociraptor`): `velociraptor/artifacts/*.yaml` of the package plus every `*.yaml` in `artifactDirs` | Set with VQL `artifact_set()` when the server's definition differs, then read back; artifacts the previous content set and this one does not are deleted (`artifact_delete`). Stored in the datastore, so no restart and no artifacts mount. An artifact the server loaded from its definitions directory (`customArtifacts`) is built in and cannot be replaced: an error until that mount is turned off. State in Secret `<statePrefix>velociraptor`. |
| `velociraptor` | One org per tenant (name = tenant name) | Created if missing; a duplicate name is an error. |
| | Keys `velociraptor-client.config.yaml` and `install-velociraptor.txt` in each enrolment bundle (reported as kind `bundle`) | The tenant org's client config as the server renders it (`orgs()` column `_client_config`, the same YAML as `velociraptor config client --org <id>`: server URLs, CA, the org nonce) and a short install hint for the matching release binary. Read after the orgs step, so a new org's config lands in the same run; a dry run reports a not-yet-created org as `create`. Created or updated when either key differs; other bundle keys kept. Needs the `enrolment` entry and `components.velociraptor`. |
| | Server monitoring table entries | Added, or their listed parameters set; unlisted parameters of that artifact are kept; other artifacts are never removed. |
| `misp` | Users listed in `components.misp.users` (kind `user`) | Created if missing (API only: random password, no e-mail); role reset on drift; another organisation or a disabled user is an error. |
| | Their auth keys (kind `user-key`) | A stored key MISP accepts as that user is kept; otherwise a new key is generated and stored in `apiKeySecretRef`. |

Client fields and how drift is handled:

- Always compared: `publicClient`, `standardFlowEnabled`, `directAccessGrantsEnabled`,
  `serviceAccountsEnabled`.
- Compared when set: `name`, `description`, `rootUrl`, `baseUrl`, `fullScopeAllowed`,
  the listed `attributes` keys, the listed protocol-mapper config keys.
- Supersets (missing entries added, extra entries never removed): `redirectUris`,
  `webOrigins`, `postLogoutRedirectUris` (the `post.logout.redirect.uris` attribute),
  `defaultClientScopes`, scope mappings, service-account roles.
- Secret: when `secretRef` is set Keycloak is made to match the Kubernetes Secret
  the application reads. The values are compared, never printed.

## What it never touches

- Human users anywhere: Keycloak users and their group/role memberships, IRIS users
  (owned by the dfir-iris `keycloakSync` CronJob), Velociraptor users and org grants
  (owned by the `Custom.Server.KeycloakSync` server artifact).
- Keycloak `default-roles-*`, built-in clients and flows (the `browser` flow is
  copied, never edited), client scopes, and anything not named in the config.
- Deletion of any kind. Removing a tenant from the config leaves its objects in
  place; clean them up by hand.
- Wazuh agents, users, roles and policies; the central indexer's remotes not in the
  config; dashboard saved objects other than missing listed index patterns (and
  `defaultIndex` when it is unset or dangling); Velociraptor artifacts other than
  the ones listed under `serverMonitoring`.

## Dry-run semantics

`--dry-run` is the default. In dry-run mode the reconciler only issues reads
(Kubernetes `get`, HTTP `GET`, read-only VQL) and reports what an apply would do:

- `create` / `update` lines mark drift; `ok` means no change; `skip` means the
  object is out of reach (e.g. an IRIS service account that does not exist);
  `error` means it could not be read or reconciled.
- Objects that depend on something the run would create first (a client in a
  realm that does not exist yet, a role mapping for a role not yet created) are
  reported as `create`/`update` without further checks.
- The `browser-mfa` plan is computed on an in-memory copy of the flow, so the dry
  run reports the same requirement changes, reorder and bind an apply would make,
  and fails verification the same way.

The last line is `N changes, M errors (...)`. Exit code 0 means no errors (drift
is allowed); 1 means at least one object failed or the config is invalid. With
`--exit-zero` object errors are still printed (plus a warning on stderr) but the
exit code is 0; an invalid config or flag still exits 1.

`--exit-zero` is meant for the Argo CD Sync hook: on a first install some
components are not up yet (e.g. a Wazuh manager still starting), and a failing
hook would block the sync that brings them up. The CronJob keeps the strict
default, so persistent errors still show as failed Jobs.

## Configuration

The input schema is documented in
[`pkg/siem/config/README.md`](../pkg/siem/config/README.md), with a full example in
[`pkg/siem/config/testdata/tenants.example.json`](../pkg/siem/config/testdata/tenants.example.json).
It contains Secret references only. Unknown fields are rejected.

Flags:

| Flag | Default | |
|---|---|---|
| `--config` | `/etc/siem/tenants.json` | Input file. |
| `--dry-run` | `true` | Report only. `--dry-run=false` applies. |
| `--only` | all | Comma-separated: `secrets,enrolment,keycloak,iris,wazuh,wazuhcentral,velociraptor,misp`. `wazuh` selects every manager; `secrets` includes `secretCopies`. |
| `--only` | all | Comma-separated: `secrets,enrolment,keycloak,iris,wazuh,wazuhcentral,retention,velociraptor`. `wazuh` selects every manager, `retention` every indexer; `secrets` includes `secretCopies`. |
| `--only` | all | Comma-separated: `secrets,enrolment,keycloak,iris,wazuh,wazuhcentral,content,velociraptor`. `wazuh` selects every manager; `secrets` includes `secretCopies`. |
| `--kubeconfig`, `--context` | in-cluster | Outside a cluster the default kubeconfig rules apply. |
| `--timeout` | `5m` | Whole run. |
| `--exit-zero` | `false` | Exit 0 even when objects could not be reconciled; errors are still reported. |
| `--interval` | `0` | Non-zero keeps the process running: reconcile every interval, then probe the health, and serve the metrics. |
| `--metrics-addr` | `:9090` | With `--interval`: listen address of `/metrics`, `/status` and `/healthz`. |
| `--probe` | `true` | With `--interval`: probe the platform health after each run. |

## Metrics and health

With `--interval` the reconciler stays up (the chart's `reconciler.mode:
deployment`) and serves three endpoints on `--metrics-addr`, on a container port
named `metrics` so a NetworkPolicy can admit Prometheus:

- `/metrics` Prometheus exposition, everything prefixed `kubesoc_`.
- `/status` JSON: last run (objects, changes, errors per component, duration,
  dry-run), last successful run, the health snapshot and an overall `healthy`
  flag. Meant for tooling such as a `siem status` command.
- `/healthz` liveness, always 200 while the process runs.

| Metric | Labels | |
|---|---|---|
| `kubesoc_reconciler_objects` | `component`, `action` | Objects of the last run per action (`ok`, `changed`, `skip`, `error`); series of components no longer reported are dropped each run. |
| `kubesoc_reconciler_runs_total` | `result` | Runs by result (`success` = no object errored). |
| `kubesoc_reconciler_last_run_timestamp_seconds`, `..._last_success_timestamp_seconds`, `..._run_duration_seconds`, `kubesoc_reconciler_dry_run` | | Last run, last error-free run, its duration, and whether the reconciler only reports. |
| `kubesoc_indexer_up`, `kubesoc_indexer_cluster_status`, `kubesoc_indexer_disk_used_percent`, `kubesoc_indexer_disk_{used,total}_bytes` | `indexer` (tenant code or `central`), `status` | From `GET /_cluster/health` and `GET /_cat/allocation`; the percentage is the fullest data node, the figure the flood-stage watermark applies to. |
| `kubesoc_wazuh_manager_up`, `kubesoc_wazuh_agents` | `tenant`, `status` | From `GET /agents/summary/status`: `active`, `disconnected`, `never_connected`, `pending`, `total`. |
| `kubesoc_wazuh_analysisd_events_dropped`, `kubesoc_wazuh_analysisd_queue_usage_max` | `tenant` | From `GET /manager/daemons/stats?daemons_list=wazuh-analysisd`: the `dropped_breakdown` leaves plus `eps.events_dropped` (a counter since the daemon started), and the fullest queue's `usage`. Absent when the manager does not serve the endpoint. |
| `kubesoc_component_up` | `component` | Keycloak (OIDC discovery of the realm), IRIS (`GET /api/ping`) and the Velociraptor API (TCP connect to `components.velociraptor.address`). |
| `kubesoc_health_last_probe_timestamp_seconds` | | Time of the last probe. |

The probes only read, and use the same Secrets the reconcile run does: each
tenant's indexer admin credentials (`INDEXER_USERNAME`/`INDEXER_PASSWORD` in
`wazuh-indexer-cred`) and its Wazuh API user. A probe that fails is recorded as
`up 0` with the reason in `/status`, never as a process error. Alerts on these
metrics ship with the `security-operations` chart (`monitoring.prometheusRule`).

`siem-reconciler publish-api-client --file <api_client.yaml> [--namespace ns]
[--name velociraptor-api-client] [--key api_client.yaml]` validates a Velociraptor
api_client config and stores it in a Secret. The Velociraptor chart runs it in an
initContainer after `velociraptor config api_client` so the reconciler always has
a current client certificate. This subcommand writes; it has no dry-run.

## RBAC

The Job's ServiceAccount needs:

- `get` on the Secrets named in the config (Keycloak admin, client secrets, IRIS
  API key, each manager's Wazuh API credentials and authd password, the indexer
  credentials, Velociraptor api_client, copy sources). Scope it with
  `resourceNames`; Secrets outside the release namespace (Keycloak, the tenant
  `wazuh-<code>` namespaces) need a Role + RoleBinding in each namespace.
- `create` on Secrets and `update` on the Secrets it writes (generated Secrets,
  copy targets, enrolment bundles, the dashboard config) in every namespace they
  live in. `create` cannot be limited by `resourceNames`.
- For `publish-api-client`: `get`, `create`, `update` on the api_client Secret.

API-side permissions: Keycloak master-realm admin, or the realm-management roles
of the `clientCredentials` client (below); an IRIS API key of a
server administrator; a MISP site admin auth key (for `components.misp`); per manager a Wazuh API user allowed to manage security
(`wazuh-wui`); an indexer user allowed to update cluster settings (and, with
API-side permissions: Keycloak master-realm admin; an IRIS API key of a
server administrator; per manager a Wazuh API user allowed to manage security
(`wazuh-wui`) and, for the metrics, to read `agent:read` and `manager:read`;
an indexer user allowed to update cluster settings (and, with
`indexPatterns`, to write saved objects and advanced settings in the dashboard's
global tenant); per tenant indexer the admin user (`wazuh-indexer-cred`), which
needs the ISM plugin and cluster-monitor permissions for the retention policies
and the health probes;
a Velociraptor api_client with the `administrator` role (needs `ORG_ADMIN` for
`org_create` and for `orgs()` to list every org with its client config, and
`COLLECT_SERVER` for `add_server_monitoring`).

## Keycloak identity

Without `keycloak.clientCredentials` every run logs in as the master-realm admin,
which can change every realm of the Keycloak. With it the reconciler logs in as the
service account of a confidential client in the SOC realm, whose rights end at
that realm and at the realm-management roles it holds. The client is created the
same way as every other client, from `clients[]`, so the usual path is:

1. Add the client to `clients[]` (the `security-operations` chart does this with
   `keycloak.reconcilerClient.enabled`): `serviceAccountsEnabled: true`,
   `secretRef` to a generated Secret, and `serviceAccountClientRoles:
   {realm-management: [...]}` with the roles the run needs: `view-realm`,
   `manage-realm` (realm settings, roles, required actions, flows), `view-users`,
   `manage-users`, `query-users`, `query-groups` (groups and their role
   mappings), `view-clients`, `manage-clients`, `query-clients` (clients, secrets,
   mappers, scopes, service-account roles), `view-identity-providers`,
   `manage-identity-providers` (tenant brokers). Keycloak only lets an identity
   grant realm-management roles it holds itself, so the client needs every role it
   hands out to other clients (e.g. `view-users` for `iris-sync`).
2. Set `keycloak.clientCredentials: {clientId, secretRef}` and keep
   `adminSecretRef`. The first run cannot log in as the client (it does not exist
   yet), falls back to the admin (reported as `keycloak login skip ...
   fallback`), and creates the client with its secret and roles.
3. The next run logs in as the client (`keycloak login ok client ...`). Once it
   does, drop `adminSecretRef` (and the reconciler's read access to the admin
   Secret). Creating a realm that does not exist yet needs the admin again.

An administrator can also create the client by hand (Keycloak admin console,
realm → Clients → Create, client authentication on, service accounts roles on,
then Service account roles → Assign role → Filter by clients → realm-management)
and store its secret in the `secretRef` Secret.

## Velociraptor gRPC without generated code

Velociraptor is AGPL-3.0 licensed, so this repository does not vendor its
`.proto` files or generated stubs. `pkg/siem/velociraptor` calls the
`proto.API/Query` streaming RPC with a raw gRPC codec and encodes the handful of
`VQLCollectorArgs` / `VQLResponse` fields it needs with `protowire`; the field
numbers are listed in `wire.go`. Everything else (orgs, org client configs, monitoring table) is
done in VQL (`orgs()`, `org_create()`, `get_server_monitoring()`,
`add_server_monitoring()`), with inputs passed as VQL environment variables rather
than spliced into the query text. No `protoc` step is needed.

## Running it by hand

From a machine that reaches the component APIs (port-forwards where needed):

```sh
make build-siem-reconciler
kubectl -n wazuh-<code> port-forward svc/wazuh 55000:55000 &
./build/siem-reconciler --config tenants.json --context <kube-context> \
  --only secrets,enrolment,keycloak,iris,wazuh
```

Keep the default dry run until the plan reads right, then rerun with
`--dry-run=false`.

## Build and image

- `make build-siem-reconciler` builds `./build/siem-reconciler`.
- `make docker-siem-reconciler` builds `ghcr.io/obmondo/siem-reconciler:<version>`
  locally from `cmd/siem-reconciler/Dockerfile` (distroless static, nonroot).
- Releases build `siem-reconciler_<Os>_<arch>` assets. The image entries in
  `.goreleaser-github.yaml` have `skip_push: true` until the release workflow
  logs in to ghcr.io.
