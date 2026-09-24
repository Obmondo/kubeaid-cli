# siem-reconciler

`siem-reconciler` makes the API-only state of a KubeAid SIEM stack (Keycloak,
DFIR-IRIS, Wazuh, Velociraptor) match one input file, `tenants.json`, rendered by
the `security-operations` Helm chart. It replaces the one-off scripts that used to
set this up by hand, and it is safe to run repeatedly: a second run against an
unchanged system prints only `ok` lines and `0 changes`.

The chart runs it as an ArgoCD PostSync Job and as a CronJob. It can also be run
from a workstation against a cluster (see [Running it by hand](#running-it-by-hand)).

## What it manages

Components run in this order; `--only` selects a subset.

| Component | Objects | Rule |
|---|---|---|
| `secrets` | Secrets listed under `secrets` | Created with random values when missing; missing keys added; existing values never changed. A Secret owned by a SealedSecret is only checked (a missing key is an error, fix the SealedSecret). |
| `keycloak` | Realm (created if missing), `bruteForceProtected`, OTP policy, `CONFIGURE_TOTP` as default required action | Only the attributes set in the config are compared. |
| | Realm roles `operators.adminRole`, `operators.analystRole`, group `operators.analystGroup` | Created if missing. |
| | Per tenant: realm role and group `<tenantGroupPrefix><code>`, group grants the role | Created if missing; other role mappings kept. |
| | Clients: settings, secret, protocol mappers, default scopes, scope mappings, service-account client roles | Update on drift for managed fields only (see below). |
| | `browser-mfa` flow | Copy `browser`, OTP sub-flow REQUIRED, its conditions DISABLED, OTP Form REQUIRED, Username Password Form first; re-read and verify; only a verified flow is bound as the realm browser flow. |
| | Optional per-tenant identity-provider broker | Created/updated; a hardcoded-group mapper puts brokered users in the tenant group. |
| `iris` | One customer per tenant (name = tenant name) | Created if missing. |
| | Service accounts listed in `components.iris.serviceAccounts` | Groups and customers (all tenants + the initial customer) added; never removed. The accounts themselves must exist. |
| `wazuh` | Rules `oidc_<adminRole>` / `oidc_<analystRole>` mapping backend roles to the stock API roles | Created, condition corrected, linked. |
| | Per tenant: policies `<g>_agents`, `<g>_group`, role `<g>_readonly` (those two + the stock read-only policies), rule `oidc_<g>` (dashes as underscores), agent group `<g>` | Created or corrected; links only added. Tenant RBAC is refused unless `rbac_mode` is `white`. Agent groups are only created with `createGroups: true`. |
| `velociraptor` | One org per tenant (name = tenant name) | Created if missing; a duplicate name is an error. |
| | Server monitoring table entries | Added, or their listed parameters set; unlisted parameters of that artifact are kept; other artifacts are never removed. |

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
- Wazuh agents, users and stock roles/policies; Velociraptor artifacts other than
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
is allowed); 1 means at least one object failed or the config is invalid.

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
| `--only` | all | Comma-separated: `secrets,keycloak,iris,wazuh,velociraptor`. |
| `--kubeconfig`, `--context` | in-cluster | Outside a cluster the default kubeconfig rules apply. |
| `--timeout` | `5m` | Whole run. |

`siem-reconciler publish-api-client --file <api_client.yaml> [--namespace ns]
[--name velociraptor-api-client] [--key api_client.yaml]` validates a Velociraptor
api_client config and stores it in a Secret. The Velociraptor chart runs it in an
initContainer after `velociraptor config api_client` so the reconciler always has
a current client certificate. This subcommand writes; it has no dry-run.

## RBAC

The Job's ServiceAccount needs:

- `get` on the Secrets named in the config (Keycloak admin, client secrets, IRIS
  API key, Wazuh API credentials, Velociraptor api_client). Scope it with
  `resourceNames`; the Keycloak admin Secret lives in the Keycloak namespace, so
  that needs its own Role + RoleBinding there.
- `create` on Secrets and `update` on the generated Secrets in the release
  namespace (only if `secrets` is used). `create` cannot be limited by
  `resourceNames`.
- For `publish-api-client`: `get`, `create`, `update` on the api_client Secret.

API-side permissions: Keycloak master-realm admin; an IRIS API key of a
server administrator; a Wazuh API user allowed to manage security (`wazuh-wui`);
a Velociraptor api_client with the `administrator` role (needs `ORG_ADMIN` for
`org_create` and `COLLECT_SERVER` for `add_server_monitoring`).

## Velociraptor gRPC without generated code

Velociraptor is AGPL-3.0 licensed, so this repository does not vendor its
`.proto` files or generated stubs. `pkg/siem/velociraptor` calls the
`proto.API/Query` streaming RPC with a raw gRPC codec and encodes the handful of
`VQLCollectorArgs` / `VQLResponse` fields it needs with `protowire`; the field
numbers are listed in `wire.go`. Everything else (orgs, monitoring table) is done
in VQL (`orgs()`, `org_create()`, `get_server_monitoring()`,
`add_server_monitoring()`), with inputs passed as VQL environment variables rather
than spliced into the query text. No `protoc` step is needed.

## Running it by hand

From a machine that reaches the component APIs (port-forwards where needed):

```sh
make build-siem-reconciler
kubectl -n <wazuh-namespace> port-forward svc/wazuh 55000:55000 &
./build/siem-reconciler --config tenants.json --context <kube-context> \
  --only secrets,keycloak,iris,wazuh
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
