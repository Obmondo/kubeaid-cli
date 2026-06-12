# Creating the `kubernetes-<cluster>` OIDC client for a workload cluster

> Companion to [`keycloak-bootstrap.md`](./keycloak-bootstrap.md). This doc
> is the operator's responsibility before running `kubeaid-cli bootstrap`
> on a workload cluster that references an external Keycloak (i.e.
> `cluster.keycloak.mode: external`).

## When this applies

A workload cluster's kube-apiserver only trusts JWTs whose audience claim
matches a known OIDC client in the referenced Keycloak realm. The
parent VPN cluster's bootstrap created `kubernetes-<vpn-cluster>` for its
own apiserver. Each workload cluster needs its own client:
`kubernetes-<workload-cluster-name>`, in the same realm.

`kubeaid-cli` does **not** call the Keycloak admin API on a workload
bootstrap — admin credentials live with the operator, network reach is a
separate problem, and a wrong-realm typo at the worst time is a real
failure mode. Manual creation is a 30-second click-through with a
predictable shape; the bootstrap prints a banner naming the exact client
ID so there's no guessing.

## Steps

1. Sign in to Keycloak admin at `https://<keycloak-dns>/admin`.
2. Switch to the realm named in `cluster.keycloak.realm`.
3. **Clients → Create client**:
   - **Client type**: OpenID Connect
   - **Client ID**: `kubernetes-<cluster-name>` (must match exactly — `cluster-name` is the value in `cluster.name` in this cluster's `general.yaml`)
   - **Name** / **Description**: free-form (e.g. "Acme staging kubelogin")
4. **Capability config**:
   - **Client authentication**: **Off** (this is a public PKCE client — no client secret)
   - **Authorization**: Off
   - **Authentication flow**: keep only **Standard flow** checked (Direct access grants, Implicit flow, Service accounts roles → unchecked)
5. **Login settings → Valid redirect URIs**, add:
   - `http://localhost:8000`
   - `http://localhost:18000`
6. **Save**.

That's it. The client now accepts `kubelogin`'s PKCE flow, and
kube-apiserver will accept JWTs whose `aud` claim is
`kubernetes-<cluster-name>`.

## Optional: assign the `api` client scope

If this workload's Keycloak realm is shared with NetBird (the typical
Obmondo setup where one VPN cluster's Keycloak hosts NetBird's clients
and every joined cluster's `kubernetes-*` clients), the parent VPN's
bootstrap already created a client scope called `api` with the NetBird
audience mapper. Assigning it to your new `kubernetes-<cluster>` client
makes kubelogin tokens carry NetBird's audience claim too — useful if
the operator wants the same token to authenticate against both NetBird
Mgmt and the cluster's kube-api.

**Clients → kubernetes-<cluster> → Client scopes → Add client scope →
`api` → Add (default)**.

Skip this step if the realm doesn't have an `api` scope or if the
workload doesn't need NetBird-side audience claims.

## Required for RBAC: the `groups` client scope

kube-apiserver reads the `groups` JWT claim to resolve a user's RBAC
groups (kubeaid renders `claimMappings.groups.claim: groups`). The
client only emits that claim if a `groups` client scope with a **Group
Membership** mapper is assigned to it. Without it, `kubelogin` is
rejected at the authorize step with `invalid_scope: ... groups ...`,
and even if it weren't, tokens would carry no `groups` claim, so
group-based RBAC silently grants nothing.

1. If the realm has no `groups` client scope yet, create one:
   **Client scopes → Create**: name `groups`, type **Default**. Add a
   **Group Membership** mapper — token claim name `groups`, **Full group
   path: Off** (so claims are `admins`, not `/admins`), include in ID +
   access token.
2. Assign it to the client: **Clients → kubernetes-&lt;cluster&gt; →
   Client scopes → Add client scope → `groups` → Add (Default)**.

## Required: users must have a verified email

kubeaid renders `claimMappings.username.claim: email`. Kubernetes'
OIDC authenticator special-cases the `email` claim: it **rejects any
token whose `email_verified` claim is not `true`** with
`oidc: email not verified` (surfaces to the user as a 401). So every
user who logs into the cluster must have their email marked verified in
Keycloak: **Users → &lt;user&gt; → Email verified: On → Save** (or have
the realm/IdP set it on creation).

This is a deliberate security posture — an unverified email is not a
trustworthy identity. If your IdP can't guarantee verified emails and
you'd rather not enforce it, the apiserver-side alternative is to render
the username as a CEL expression (`username.expression: "claims.email"`,
which skips the `email_verified` check) instead of the `claim` shorthand
— but that weakens the guarantee and is not the default.

## RBAC: bind groups, not users

Once a user authenticates, they have **no permissions** until something
grants them — otherwise every call is `403 Forbidden`. Bind a Keycloak
**group** (not individual users) to a ClusterRole, so onboarding a user
is just adding them to the group in Keycloak — no per-user `kubectl`:

```bash
kubectl create clusterrolebinding <cluster>-oidc-admins \
  --clusterrole=cluster-admin --group=<keycloak-group>
```

For shared clusters, ship these group→role bindings as manifests in the
kubeaid-config repo so ArgoCD applies them declaratively, rather than
running `kubectl` by hand per cluster.

## Obmondo SRE access (`obmondo.monitoring: true`)

When a cluster sets `obmondo.monitoring: true`, kubeaid renders a **second**
`jwt:` issuer into the apiserver's AuthenticationConfiguration: Obmondo's
central Keycloak. This lets Obmondo SREs `kubectl` into the cluster with their
Obmondo identity, without the customer issuing them an account in the customer
realm. It's one-way — the customer's Keycloak never learns about Obmondo's,
and the entry is independent of `cluster.keycloak` (a cluster can trust the
Obmondo issuer with no customer issuer at all).

The client is created the same way as the customer one above, but **in
Obmondo's Keycloak**, with two differences that each surface as a bare `401
Unauthorized` from kubectl if you get them wrong:

- **Issuer URL** — kube-apiserver trusts
  `https://keycloak.obmondo.com/auth/realms/Obmondo`
  (`constants.ObmondoKeycloakIssuerURL`). Mind the `/auth` base path and the
  `Obmondo` realm casing: the token's `iss` is matched against it
  byte-for-byte.
- **Client ID = the cluster name**, *not* `kubernetes-<cluster>`. The Obmondo
  `jwt:` entry trusts `audiences: [<cluster.name>]`, so the client must be
  named exactly `cluster.name` (e.g. a cluster named `staging` → client
  `staging`) for the token's `aud` to match.

Everything else mirrors the customer client: OpenID Connect, **Client
authentication: Off** (public PKCE), **Standard flow** only, redirect URIs
`http://localhost:8000` + `http://localhost:18000`, the `groups` client scope,
and verified emails — the same `groups`-claim and `email_verified` rules from
the sections above apply in Obmondo's realm too. SRE users still need an RBAC
binding (bind an Obmondo group to a ClusterRole).

On the client side, add it to the cluster's klist entry as a second issuer so
`kubeaid-cli login` offers it:

```yaml
oidc:
  - name: customer
    issuerUrl: https://keycloak.vpn.acme.com/realms/acme
    clientId: kubernetes-staging
  - name: obmondo-sre
    issuerUrl: https://keycloak.obmondo.com/auth/realms/Obmondo
    clientId: staging          # = cluster.name, NOT kubernetes-staging
```

`login` then prompts which issuer to use (or pass `--issuer obmondo-sre`).
Clusters bootstrapped before the issuer URL was corrected
(`…/realms/obmondo` → `…/auth/realms/Obmondo`) still carry the old value in
their live `auth-config.yaml` — re-render + sync, or fix `issuer.url` in place
on every control-plane node, so it matches the token's `iss`.

## What `kubeaid-cli bootstrap` checks

Before provisioning infrastructure, `kubeaid-cli` probes the realm's
discovery endpoint:

```
GET https://<keycloak-dns>/realms/<realm>/.well-known/openid-configuration
```

A reachable realm with a matching `issuer:` field is a hard prerequisite
— the probe fails fast on DNS / TLS / 404 errors before anything else
runs (see [`pkg/config/parser/oidc_discovery.go`](../pkg/config/parser/oidc_discovery.go)).

The probe does NOT verify the client exists. That check happens later,
when the operator runs `kubeaid-cli login` after bootstrap; `kubelogin`
fails clearly with `error: oauth2: invalid_client` if the
`kubernetes-<cluster>` client is missing. Fix it in Keycloak admin UI
and rerun `kubeaid-cli login`.

## Re-running on existing clusters

`kubeaid-cli` doesn't care whether the client was created before or
after bootstrap, only that it exists when `kubelogin` first runs. If you
forget to create it pre-bootstrap, add it after-the-fact — no
cluster-side change required.

## What about workload clusters WITHOUT a `cluster.keycloak` block?

Omitting the block boots the cluster with no OIDC trust. kube-apiserver
falls back to the only thing it has: the static `admin.conf` kubeconfig
written into `~/.kube/<cluster>.conf` by `kubeaid-cli`. That's fine for
solo / dev clusters but **not** for shared production access — every
user has full cluster-admin rights and there's no per-user audit trail.

The bootstrap prints a warning when it detects this shape. Operators
who deliberately want admin.conf-only access can ignore it; everyone
else should add a `cluster.keycloak` block and follow the steps above.
