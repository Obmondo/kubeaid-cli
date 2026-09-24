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
| `tenantGroupPrefix` | string | no (`tenant-`) | `prefix + code` names the tenant's Keycloak group and realm role, Wazuh agent group, Wazuh policies `<g>_agents` / `<g>_group`, role `<g>_readonly` and rule `oidc_<g with - as _>`. Lowercase letters, digits, dashes. |
| `keycloak` | object | yes | See below. |
| `operators` | object | yes | `adminRole`, `analystRole` (realm roles), `analystGroup` (group). Empty entries are skipped. |
| `tenants` | list | yes | See below. |
| `clients` | list | no | Keycloak OIDC clients. |
| `secrets` | list | no | Secrets created with random values when missing. |
| `components` | object | yes | `iris`, `wazuh`, `velociraptor`; an absent component is skipped. |

## `keycloak`

| Field | Type | Meaning |
|---|---|---|
| `url` | string, req. | Keycloak root incl. context path, e.g. `http://keycloakx-http.keycloakx.svc/auth`. |
| `realm` | string, req. | Realm to manage (created if missing). |
| `adminUsername` | string | Master-realm admin, default `admin`. |
| `adminSecretRef` | SecretRef, req. | The admin password. |
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
group granting the role, IRIS customer `name`, Velociraptor org `name`, Wazuh
agent group, policies, role and rule.

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

`{namespace, name, keys: [{key, generator}]}` with `generator` one of
`password` (default, 32 alphanumerics), `hex32`, `base64-32`. Missing Secrets are
created; missing keys are added to Secrets the reconciler may write to. Existing
values are never changed; Secrets owned by a SealedSecret are only checked.

## `components`

- `iris`: `url`, `apiKeySecretRef` (key of an IRIS server administrator),
  `caFile`, `insecureSkipVerify`, `initialCustomer` (default `IrisInitialClient`),
  `serviceAccounts: [{login, groups}]` (existing IRIS logins; groups and every
  tenant customer plus `initialCustomer` are added, never removed).
- `wazuh`: `url`, `credSecretRef: {namespace, name, usernameKey (API_USERNAME),
  passwordKey (API_PASSWORD)}`, `caFile`, `insecureSkipVerify` (default false),
  `adminApiRole` (default `administrator`), `analystApiRole` (default `readonly`),
  `createGroups` (create missing agent groups through the API).
- `velociraptor`: exactly one of `apiClientSecretRef` (key default
  `api_client.yaml`) and `apiClientFile`; `address` overrides the api_client's
  `api_connection_string`; `serverMonitoring: [{artifact, parameters}]` must be
  running (only listed parameters compared; other artifacts are never removed).
