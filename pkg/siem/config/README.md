# siem-reconciler input: `tenants.json`

The `security-operations` chart renders this file into a ConfigMap that the
reconciler mounts at `/etc/siem/tenants.json` (`--config`). It holds **secret
references only**, never secret values. Unknown fields are rejected, so the chart
and the reconciler must agree on this schema; a complete example is
[`testdata/tenants.example.json`](testdata/tenants.example.json).

A `SecretRef` is `{"namespace", "name", "key"}` and points at one key of a
Kubernetes Secret.

## Top level

| Field | Type | Req. | Meaning |
|---|---|---|---|
| `domain` | string | yes | Base DNS domain (informational). |
| `tenantGroupPrefix` | string | no (`tenant-`) | `prefix + code` names the tenant's Keycloak group and realm role (the OpenSearch backend role) and the rule `oidc_<g with - as _>` on the tenant's Wazuh manager. Lowercase letters, digits, dashes. |
| `keycloak` | object | yes | See below. |
| `operators` | object | yes | `adminRole`, `analystRole` (realm roles), `analystGroup` (group). Empty entries are skipped. |
| `tenants` | list | yes | See below. |
| `clients` | list | no | Keycloak OIDC clients. |
| `secrets` | list | no | Secrets created with random values when missing. |
| `secretCopies` | list | no | Single Secret keys copied into other Secrets, see below. |
| `components` | object | yes | `iris`, `wazuh`, `wazuhCentral`, `velociraptor`, `misp`; an absent component is skipped. |
| `enrolment` | list | no | Per-tenant agent enrolment bundle Secrets, see below. |

## `keycloak`

| Field | Type | Meaning |
|---|---|---|
| `url` | string, req. | Keycloak root incl. context path, e.g. `http://keycloakx-http.keycloakx.svc/auth`. |
| `realm` | string, req. | Realm to manage (created if missing). |
| `adminUsername` | string | Master-realm admin, default `admin`. |
| `adminSecretRef` | SecretRef | The admin password. Required without `clientCredentials`; with them it is only the fallback used while the client cannot log in (the run that creates it). |
| `clientCredentials` | `{clientId, secretRef}` | Log in as the service account of this confidential client in `realm` (client-credentials grant) instead of as the master-realm admin. The run reports which identity it used (kind `login`: `ok` for the client or an admin-only config, `skip` with the reason when it fell back to the admin). |
| `caFile`, `insecureSkipVerify` | string, bool | TLS to Keycloak. |
| `bruteForceProtected` | bool | Omitted = left alone. |
| `otpPolicy` | `{type, algorithm, digits, period}` | Omitted = left alone. |
| `requireTOTP` | bool | `CONFIGURE_TOTP` enabled + default action. |
| `mfaFlow` | `{alias, copyFrom, bind}` | Mandatory OTP browser flow; defaults `browser-mfa`, `browser`, `true`. Omitted = flows untouched. |

## `tenants[]`

| Field | Type | Meaning |
|---|---|---|
| `code` | string, req., unique | `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, e.g. `"001"`. |
| `name` | string, req., unique | Display name; also the IRIS customer and Velociraptor org name. |
| `retentionDays` | int | Index retention, used by the chart (not by the reconciler). |
| `idp` | object | Optional broker: `alias` (default `<group>-idp`), `displayName` (default `name`), `providerId` (default `oidc`), `enabled` (default true), `trustEmail`, `config` (map of Keycloak IdP config keys; only listed keys are compared), `clientSecretRef` (used on create only). A hardcoded-group mapper puts every brokered user in the tenant group. |

Per tenant the reconciler ensures: Keycloak group + realm role `<group>` with the
group granting the role, IRIS customer `name` and Velociraptor org `name`. Its
Wazuh manager (if listed under `components.wazuh`) gets the rule mapping the group
to `tenantApiRole`.

## `clients[]`

`clientId` (req., unique), `name`, `description`, `publicClient`,
`standardFlowEnabled`, `directAccessGrantsEnabled`, `serviceAccountsEnabled`
(booleans default false and are always compared), `fullScopeAllowed` (compared
when set), `rootUrl`, `baseUrl`, `attributes` (only listed keys compared),
`redirectUris`, `webOrigins`, `postLogoutRedirectUris`, `defaultClientScopes`
(**supersets**: missing entries added, others kept), `protocolMappers`
(`[{name, protocolMapper, config}]`, directly on the client; only listed config
keys compared), `scopeMappings` (`{realm: [role], clients: {clientId: [role]}}`),
`serviceAccountClientRoles` (`{clientId: [role]}`), `secretRef` (Keycloak is made
to match this Secret; omit to leave the secret alone).

## `secrets[]`

`{namespace, name, keys: [{key, generator, value}]}` with `generator` one of
`password` (default, 32 alphanumerics), `hex32`, `base64-32`, or a literal `value`
(e.g. an OIDC client id next to its generated secret; `generator` is then
ignored). Secrets may live in any namespace, including tenant namespaces. Missing Secrets are
created; missing keys are added to Secrets the reconciler may write to. Existing
values are never changed; Secrets owned by a SealedSecret are only checked.

## `secretCopies[]`

`{from: SecretRef, to: SecretRef}`. The `to` key is created, or overwritten when
its bytes differ from `from` (other keys of the target Secret are kept; a missing
target Secret is created). Targets are unique and never the source. A missing
source is an error for that entry only. Values are compared, never printed. Run as
part of the `secrets` component, after generation, so a copy may name a generated
Secret as its source. Credentials sealed into the tenant namespaces are only read
by the reconciler, never generated or copied by it.

## `components`

- `iris`: `url`, `apiKeySecretRef` (key of an IRIS server administrator),
  `caFile`, `insecureSkipVerify`, `initialCustomer` (default `IrisInitialClient`),
  `groups: [{name, description, permissions}]` (IRIS groups the reconciler owns:
  created when missing, permission mask reset when it drifted; `permissions` are
  IRIS 2.4 names such as `alerts_write`, `customers_read`, never
  `server_administrator`),
  `serviceAccounts: [{login, groups, create, name, email, apiKeySecretRef,
  customers}]`: groups are added, never removed. Without `customers` the account
  gets every tenant customer plus `initialCustomer` (added, never removed); with
  `customers` (tenant names, or `initialCustomer`) it has exactly those, and any
  other customer is removed. `create: true` adds a missing login as an IRIS
  service account; `apiKeySecretRef` keeps its API key in that Secret key.
- `wazuh`: a **list**, one Wazuh manager per tenant (each manager belongs to one
  tenant, usually in namespace `wazuh-<code>`). Per entry: `tenant` (req., a code
  from `tenants`, at most one entry per tenant), `url`, `credSecretRef: {namespace,
  name, usernameKey (API_USERNAME), passwordKey (API_PASSWORD)}`, `caFile`,
  `insecureSkipVerify` (default false), `adminApiRole` (default `administrator`),
  `analystApiRole` (default `readonly`), `tenantApiRole` (default `readonly`). The
  reconciler maps `operators.adminRole` to `adminApiRole`, `operators.analystRole`
  to `analystApiRole` and the tenant group to `tenantApiRole`, each through a rule
  `oidc_<role>` on the backend role. The API roles must exist.
- `wazuhCentral`: the central search-only OpenSearch indexer. `url`,
  `credSecretRef: {namespace, name, usernameKey (INDEXER_USERNAME), passwordKey
  (INDEXER_PASSWORD)}` (basic auth), `caFile`, `insecureSkipVerify`,
  `remotes: [{alias, seeds}]` (cross-cluster search connections; `alias` matches
  `^[a-z0-9_-]+$` and is unique, `seeds` is a non-empty list of `host:port`
  transport addresses). Each remote is set as the persistent cluster setting
  `cluster.remote.<alias>.seeds`; remotes not listed are reported, never removed.
  Optional `dashboardConfigSecret: {namespace, name}`: a Secret whose one key
  `wazuh.yml` is the Wazuh dashboard app config listing every `wazuh` manager
  (URL without port, port, API credentials copied from its `credSecretRef`,
  `run_as: true`). The first host id is `1513629884013` (the dashboard image's
  start script expects it), the others `t<code>`.
  Optional `dashboardURL` (the central OpenSearch Dashboards with the Wazuh app,
  e.g. `http://wazuh-dashboard:5601`) and `indexPatterns: [{title, timeFieldName
  (default `timestamp`), default}]`: saved index patterns to ensure on that
  dashboard, such as `*:wazuh-alerts-*` for cross-cluster search (the Wazuh app
  never creates it because no local index matches). `indexPatterns` needs
  `dashboardURL`; titles are unique; at most one pattern has `default: true`. The
  reconciler signs in with `credSecretRef` (and `caFile`/`insecureSkipVerify`),
  creates a pattern only when none with that exact title exists (existing ones are
  never modified), and sets the dashboard's `defaultIndex` to the default pattern
  only when it is unset or points to a pattern that no longer exists.
- `velociraptor`: exactly one of `apiClientSecretRef` (key default
  `api_client.yaml`) and `apiClientFile`; `address` overrides the api_client's
  `api_connection_string`; `serverMonitoring: [{artifact, parameters}]` must be
  running (only listed parameters compared; other artifacts are never removed).

- `misp`: `url`, `apiKeySecretRef` (auth key of a MISP site admin), `caFile`,
  `insecureSkipVerify`, `users: [{email, role (default "Read Only"), org (default:
  the admin key's own organisation), apiKeySecretRef}]`. A missing user is created
  (random password nobody keeps, no e-mail sent) in that role and organisation; a
  user in another role is put back into it; a disabled user or one in another
  organisation is an error. A stored key is kept when MISP accepts it *as that
  user* (`/users/view/me`), otherwise a new key is generated
  (`/users/resetauthkey`, with or without advanced auth keys) and stored. The key
  Secret must not be the admin key's.

## `enrolment[]`

Per tenant a Secret an agent installer can be handed. Created when missing and
updated when its rendered content differs; it holds no generated value of its own.

| Field | Type | Meaning |
|---|---|---|
| `tenant` | string, req. | A code from `tenants`. |
| `namespace`, `name` | string, req. | The bundle Secret; unique. |
| `managerHost` | string, req. | Host agents connect to. |
| `registrationPort` | int, req. | authd enrolment port (1-65535). |
| `eventsPort` | int, req. | Agent events port (1-65535). |
| `agentVersion` | string | Wazuh agent package version the scripts install, default `4.14.8-1`. Must not be newer than the manager. |
| `authdSecretRef` | SecretRef, req. | The manager's authd password, copied into the bundle. A missing Secret is an error for this bundle only. |
| `caSecretRef` | SecretRef | CA of the manager's authd certificate (e.g. `ca.crt` of its cert-manager Secret). A missing Secret leaves the CA out (reported in the detail); a Secret without the key is an error. |

Bundle keys: `manager_host`, `registration_port`, `events_port`, `authd.pass`,
`manager-ca.pem` (with a CA), and the install scripts `install-linux.sh` (deb or
rpm), `install-windows.ps1` (MSI) and `install-macos.sh` (pkg). The scripts install
the Wazuh agent `agentVersion` from packages.wazuh.com and set
`WAZUH_MANAGER`, `WAZUH_MANAGER_PORT`, `WAZUH_REGISTRATION_SERVER`,
`WAZUH_REGISTRATION_PORT` and `WAZUH_REGISTRATION_PASSWORD`; agent groups are not
used. With a CA they write it to `/etc/wazuh-manager-ca.pem`
(`%ProgramData%\wazuh-manager-ca.pem` on Windows) and set `WAZUH_REGISTRATION_CA`,
so the agent verifies the manager's certificate and the host name it dials when
it enrols. The packages are verified before they are installed: deb from the
Wazuh apt repository (`signed-by` the Wazuh key, the source removed again after
the install), rpm with `rpm -K` against the imported Wazuh key, the key's
fingerprint checked where gpg can show it; the macOS pkg and the Windows MSI
against the SHA-512 published under packages.wazuh.com/4.x/checksums and their
Developer ID (Wazuh Inc, team KLZK8P68R5) or Authenticode (`Wazuh, Inc`)
signature. With `components.velociraptor` set, the velociraptor step also writes
`velociraptor-client.config.yaml` (the client config of the org named like the
tenant) and `install-velociraptor.txt` (install hint) into the bundle.
