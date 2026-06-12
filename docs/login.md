# Logging into clusters with `kubeaid-cli login`

> Companion to [`workload-cluster-keycloak.md`](./workload-cluster-keycloak.md)
> (the cluster-side OIDC prerequisites) and
> [`netbird-vpn-architecture.md`](./netbird-vpn-architecture.md) (how
> clusters become reachable over the mesh in the first place).

`login` turns "I want kubectl against cluster X" into one command. It
resolves a cluster from your local **klist registry**, merges a
ready-to-use entry into your kubeconfig (OIDC via `kubelogin`, other
contexts preserved), switches `current-context`, and warms the token
cache so the browser dance happens now rather than on your first
`kubectl` call.

It is deliberately self-contained: **no Docker, no `general.yaml`, no
`secrets.yaml`**. Everything it needs is the registry clone, the
`kubelogin` binary, and (for interactive mode) a connected NetBird
daemon.

## Prerequisites

1. **`kubelogin`** on your PATH — <https://github.com/int128/kubelogin>.
   Missing binary fails with install guidance; `--no-authenticate`
   writes the kubeconfig without it.
2. **A klist registry clone** at `~/.config/klist` (override with
   `--registry` / `KUBEAID_REGISTRY`). Layout below.
3. **Interactive mode only**: the NetBird daemon installed and
   `Connected` (`netbird up`).
4. **Cluster-side** (operator's job, once per cluster — see
   [`workload-cluster-keycloak.md`](./workload-cluster-keycloak.md)):
   the `kubernetes-<cluster>` Keycloak client, the `groups` client
   scope, verified user emails, and a group→role RBAC binding.

## The three modes

| invocation | mode |
|---|---|
| `kubeaid-cli login` | **Interactive.** Queries the NetBird daemon for reachable cluster peers, intersects with the registry, and shows a customer → cluster picker. |
| `kubeaid-cli login staging.acme` | **Direct.** Skips the picker; goes straight to the `staging` entry under customer `acme`. Fast re-entry — a cached token means no browser at all. |
| `kubeaid-cli login --cert /path/to/cert.pem` | **Non-interactive (CI).** Derives `<cluster>.<customer>` from the puppet cert's Subject CN. Also via `KUBEAID_CERT`. Mutually exclusive with the positional argument. |

In interactive mode, a cluster counts as "reachable" when a connected
NetBird peer's FQDN matches `<prefix><cluster-name><suffix>` — by
default `k8s-` and `.netbird`, both configurable in the registry's
`global.yaml`. If **nothing** is reachable (0 peers, or no access
policy yet), login falls back to offering the whole registry and always
prompts — it never silently auto-authenticates against a sole entry.

## Flags and environment

| flag | env | default | purpose |
|---|---|---|---|
| `--registry` | `KUBEAID_REGISTRY` | `~/.config/klist` | path to the klist clone |
| `--kubeconfig` | `KUBECONFIG` | `~/.kube/config` | kubeconfig to merge into |
| `--cert` | `KUBEAID_CERT` | *(unset)* | puppet cert PEM for non-interactive mode |
| `--no-authenticate` | — | `false` | write the kubeconfig, skip the kubelogin OIDC step |
| `--issuer` | — | *(unset)* | for a cluster with multiple issuers, pick one by `name` without the prompt; in `--cert`/CI mode with >1 issuer and no flag, the first-listed issuer is used |

## The klist registry

```
~/.config/klist/
├── global.yaml                    # optional, deployment-wide settings
└── clusters/
    └── acme/                      # one directory per customer
        ├── _customer.yaml         # optional shared OIDC defaults
        └── staging.yaml           # one file per cluster
```

`global.yaml` (all fields optional):

```yaml
netbird:
  managementUrl: https://netbird.vpn.acme.com   # sanity-checked against the daemon (mismatch warns)
  clusterPeerPrefix: "k8s-"                     # default
  clusterPeerSuffix: ".netbird.selfhosted"      # default is ".netbird"
contextPrefix: ""                               # e.g. "kubeaid-" → contexts named kubeaid-staging.acme
```

A cluster file:

```yaml
name: staging                     # identity — may differ from the filename (see below)
server: https://203.0.113.10:6443
caBundle: |
  -----BEGIN CERTIFICATE-----
  …the cluster CA, PEM…
  -----END CERTIFICATE-----
oidc:                             # a list — one entry per issuer the apiserver trusts
  - name: customer                # picker label; required + unique when >1, optional for a lone issuer
    issuerUrl: https://keycloak.vpn.acme.com/realms/acme
    clientId: kubernetes-staging
    # groupsClaim:   defaults to "groups"
    # usernameClaim: defaults to "email"
  - name: sre                     # a second issuer the same cluster also trusts
    issuerUrl: https://keycloak.acme.com/auth/realms/Ops
    clientId: staging
```

Semantics worth knowing:

- **Identity is the in-YAML `name:` field**, falling back to the
  filename stem. This lets a cluster be renamed to track its NetBird
  peer FQDN without renaming files.
- **`oidc` is a list of issuers.** `_customer.yaml` issuers (if any) are
  **prepended** to the cluster's own — a shared issuer lives once at the
  customer level. There is no per-field merge of a single issuer anymore;
  each entry is self-contained.
- **Choosing an issuer.** With one issuer, login uses it silently. With
  more than one, login prompts you to pick (interactive); `--issuer <name>`
  selects without prompting; `--cert`/CI uses the first-listed issuer.
- After merging, the cluster needs `name`, `server`, `caBundle`, and at
  least one issuer; each issuer needs `issuerUrl` + `clientId`, plus a
  unique `name` when there is more than one — validation names exactly
  what's missing.
- A typo'd `login bogus.acme` errors with the full list of clusters
  that *do* exist in the registry.

## What gets written

One cluster + context + user trio, all named
`<contextPrefix><cluster>.<customer>` (e.g. `staging.acme`), upserted
by name — re-running updates in place, other entries are untouched,
and `current-context` switches to it. The user entry is a `kubelogin`
exec block:

```yaml
users:
  - name: staging.acme
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1beta1
        command: kubelogin
        args:
          - get-token
          - --oidc-issuer-url=https://keycloak.vpn.acme.com/realms/acme
          - --oidc-client-id=kubernetes-staging
          - --oidc-extra-scope=email
          - --oidc-extra-scope=groups
```

The exec args carry the **issuer you selected**; re-running login and
choosing a different issuer overwrites the context (kubelogin caches tokens
per issuer+client, so both still coexist on disk). The same argv drives the
immediate cache-warming run, so the two paths cannot drift. Tokens land in
kubelogin's cache (`~/.kube/cache/oidc-login/`); subsequent `kubectl` calls
reuse them until expiry. The file is written atomically with mode `0600`.

One caveat: the kubeconfig is re-marshalled through a minimal model of
the four standard sections — exotic top-level fields some tools add
(`preferences`, `extensions`, …) are dropped on write. For
kubectl-managed configs this is a no-op.

## First login, end to end

```console
$ kubeaid-cli login staging.acme
kubeconfig written to /home/me/.kube/config (cluster: staging.acme)
Open the following URL in your browser: http://localhost:8000
# … Keycloak SSO in the browser …
authenticated; token cached

$ kubectl get nodes
```

Before opening the browser, login pre-flights the chosen issuer's
`.well-known/openid-configuration` — a wrong `oidc.issuerUrl` (typo, missing
`/auth` base path, realm-name casing) fails fast here, naming the realm's
canonical issuer when the document loads, instead of surfacing cryptically
mid-flow.

If the browser step fails, the kubeconfig is already on disk — any
`kubectl` command retries kubelogin, or re-run with
`--no-authenticate` to skip OIDC entirely.

## Troubleshooting

| symptom | meaning / fix |
|---|---|
| `issuer hostname is not resolvable` | typo in the cluster file's `oidc.issuerUrl`, or DNS missing |
| `DNS lookup failed (server misbehaving)` | local DNS / NetBird mesh DNS problem |
| `issuer is not listening on that address` | Keycloak down, or wrong port in `issuerUrl` |
| `issuer reachable but did not respond in time` | slow network / mesh path |
| `TLS error reaching issuer` | Keycloak cert not trusted by your system store |
| `issuer returned HTTP 404 …` | the `oidc.issuerUrl` path is wrong — check the `/auth` base path and the realm name's casing |
| `issuer mismatch — … canonical issuer is X` | `oidc.issuerUrl` resolves but the realm declares a different issuer; set it to the canonical value shown |
| `oauth2: invalid_client` | the `kubernetes-<cluster>` client doesn't exist in the realm yet — create it ([guide](./workload-cluster-keycloak.md)) |
| `invalid_scope: … groups …` | the realm/client is missing the `groups` client scope |
| `oidc: email not verified` (as a 401 from kubectl) | the user's email isn't marked verified in Keycloak |
| authenticated but every kubectl call is `403 Forbidden` | authN works — you just lack RBAC. Bind your user (`kind: User`, name = your `email` claim) or a token group to a (Cluster)Role |
| `Unauthorized` from kubectl with a *valid* token | the apiserver has no `jwt:` entry matching this token. Decode the token (`iss`, `aud`) and compare to the cluster's AuthenticationConfiguration: an entry's `issuer.url` must equal `iss` **exactly** (watch the `/auth` base path and realm casing) and its `audiences` must include `aud`. On a multi-CP control plane, fix the file on **every** CP — intermittent 401 = one CP still wrong |
| stale/confusing token state | `rm -rf ~/.kube/cache/oidc-login/` and re-run login |
| `netbird daemon is "Disconnected"` | `netbird up` first (interactive mode only) |

Each kubelogin failure is also printed verbatim above the categorised
error, so the table's left column is what you'll see on the final
`ERROR` line.
