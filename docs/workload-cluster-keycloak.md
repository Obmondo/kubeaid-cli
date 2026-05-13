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
