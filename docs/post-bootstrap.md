# Post-bootstrap operator guide — VPN cluster

> When `kubeaid-cli bootstrap` finishes for a managed-Keycloak VPN cluster,
> it prints a "Bootstrap complete — next steps" panel with the URLs and
> the Keycloak admin password. This doc is the longer-form companion:
> what you actually do next as an operator to start onboarding users.
>
> Architecture-level companion: [keycloak-bootstrap.md](./keycloak-bootstrap.md),
> [netbird-vpn-architecture.md](./netbird-vpn-architecture.md).

## What kubeaid-cli left behind

By the time bootstrap returns, the cluster has:

| Component | Where | Who manages it |
|---|---|---|
| Keycloak admin console | `https://keycloak.<vpn-dns>/auth/admin/` | you (this doc) |
| NetBird dashboard | `https://netbird.<vpn-dns>/` | you (this doc) |
| Realm `<derived>` (e.g. `obmondo` for `obmondo.com`) | Keycloak | kubeaid-cli created it |
| OIDC client `netbird-client` (public, device flow + PKCE) | Keycloak realm | kubeaid-cli |
| OIDC client `netbird-backend` (confidential, service account) | Keycloak realm | kubeaid-cli |
| OIDC client `kubernetes-<cluster>` (public, PKCE — for kubelogin) | Keycloak realm | kubeaid-cli |
| Client scope `api` (audience mapper → `netbird-client`) | Keycloak realm | kubeaid-cli |
| Client scope `groups` (Group Membership mapper → `groups` claim) | Keycloak realm | kubeaid-cli |
| kube-apiserver public LB | HCloud | **disabled** at finalize — reach the API through the NetBird mesh |
| Traefik ingress LB | HCloud | public — that's how operators reach the Keycloak / NetBird UIs |

Nothing else is gated by the public kube-apiserver anymore, so once
you've finished step 1 below you can close your laptop and forget the
kubectl context.

## Step 1 — Sign in to Keycloak admin and create the first user

Open the **Console** URL from the bootstrap panel and sign in with `admin`
+ the password the panel printed. (If you missed it: ssh through the NAT
gateway and run `kubectl get secret -n keycloakx keycloak-admin -o jsonpath='{.data.KEYCLOAK_PASSWORD}' | base64 -d`.)

Then:

1. Top-left realm dropdown → switch from `master` to your cluster's realm
   (the derived one, e.g. `obmondo`). **Every step below assumes you're
   in this realm, not `master`.**
2. **Users** → **Add user**.
   - Username, email, first/last name as usual.
   - **Email verified: On** (otherwise the user is stuck on a "verify
     your email" page that won't work without an SMTP server).
   - **Save**.
3. **Credentials** tab → **Set password**. Untick *Temporary* unless you
   want to force a password change on first login.

Repeat for each operator who needs access.

> **Federation alternatives** — manual user-by-user creation is fine for
> a tiny team. For anything bigger, federate the realm to your existing
> identity source instead:
>
> - **Identity-provider federation**: Realm → Identity providers →
>   add Google / Microsoft Entra / GitHub / generic OIDC / SAML. Users
>   sign in with their upstream IdP; Keycloak auto-creates a local
>   shadow account on first login.
> - **LDAP / AD user federation**: Realm → User federation → Add LDAP.
>   Keycloak proxies the existing directory; "Add user" stays in your
>   AD.
> - **Self-registration**: Realm settings → Login → User registration:
>   On. Adds a "Register" link to the login page. Combine with the
>   *Email verified* requirement and the JWT allow-groups gate below
>   so random signups can't auto-join the mesh.
>
> kubeaid-cli stays out of this on purpose — every customer's identity
> story is different.

## Step 2 — (Optional) Set up groups for access control

Skip this if you have one or two users and want everyone to have
everything. Come back to it the moment you need "this person can join
the mesh but isn't a cluster admin".

Both NetBird and kube-apiserver read group membership from the same JWT
`groups` claim — the `groups` client scope kubeaid-cli already wired up
makes sure every issued token carries it. So **one Keycloak group can
drive both mesh access and kube-API RBAC** without you maintaining two
sources of truth.

1. Realm sidebar → **Groups** → **Create group**.
2. Pick a name. Suggestions:
   - `mesh-users` — anyone allowed to join NetBird.
   - `cluster-admins` — full kube-apiserver cluster-admin.
   - `cluster-viewers` — read-only kube-apiserver access.
3. **Users** → pick a user → **Groups** tab → **Join Group** → select the
   group.

To use these groups:

**For NetBird mesh access**, in the NetBird Dashboard at
`https://netbird.<vpn-dns>/`:

1. Settings → Groups → **Enable JWT group sync**.
2. Set **JWT claim** to `groups`.
3. Set **JWT allow groups** to the names of the Keycloak groups you
   want to admit (e.g. `mesh-users`). NetBird Mgmt will only accept
   tokens whose `groups` claim intersects that list — anyone else's
   `netbird up` is rejected by the management API.

**For kube-apiserver RBAC**, write a `ClusterRoleBinding` (or
`RoleBinding` for a single namespace) referencing the Keycloak group
by name. Example: grant `cluster-admin` to anyone in `cluster-admins`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: keycloak-cluster-admins
subjects:
  - kind: Group
    name: cluster-admins        # matches the JWT `groups` claim
    apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: cluster-admin
  apiGroup: rbac.authorization.k8s.io
```

Apply it through kubeaid-config (the same way every other in-cluster
manifest is managed); it'll sync along with the rest of the apps.

## Step 3 — Join the NetBird mesh

On every machine that should be on the mesh:

1. Install the NetBird client: <https://docs.netbird.io/how-to/installation>.
2. Run:
   ```
   netbird up --management-url https://netbird.<vpn-dns>
   ```
3. NetBird CLI opens a browser (OAuth 2.0 Device Authorization Grant,
   RFC 8628). Sign in as the Keycloak user from Step 1.
4. Once authenticated, the CLI hands the access token to NetBird Mgmt,
   which:
   - validates the JWT (signature, audience = `netbird-client`,
     expiry);
   - looks up the user in its own postgres by the JWT `sub` claim;
   - on first login: creates a NetBird-side user record (Keycloak is
     not modified — direction is read-only `NetBird → Keycloak`);
   - on subsequent logins: reuses the existing record;
   - if JWT allow-groups is configured (Step 2): rejects the token
     when the `groups` claim doesn't intersect the allow list.
5. The peer is now on the mesh. `netbird status` from another peer
   should see it.

### Headless / server peers

The same `netbird up` works on a server with no browser:

- The CLI prints a URL + user code; you open the URL anywhere with a
  browser, sign in, paste the code, and the server-side CLI completes
  the flow.
- For fully automated joins (CI, image bake-time), generate a **Setup
  Key** in the NetBird Dashboard (Settings → Setup Keys → New Setup Key)
  and pass it via `netbird up --setup-key <key>`. Setup keys bypass
  OIDC entirely — they're how unattended peers join.

## Step 4 — Get kubectl working from the mesh

The kube-apiserver's public IP is disabled by now (see the table at the
top) so kubectl from your laptop won't reach it directly. The path:

1. Make sure your laptop is on the NetBird mesh (Step 3).
2. Use the kubeconfig kubeaid-cli wrote to `outputs/kubeconfigs/clusters/main.yaml`.
   Its server URL is the cluster's API FQDN (e.g.
   `https://api.vpn.<dns>:6443`); the DNS record points at the LB's
   private IP, and the mesh routes you there.
3. Configure the kubeconfig's user to use kubelogin against the
   `kubernetes-<cluster>` Keycloak client. The exact config lives in
   [hetzner-hcloud-vpn-cluster.md](./hetzner-hcloud-vpn-cluster.md).
   First `kubectl` opens a browser, completes OIDC, caches the token —
   subsequent commands are silent.

## What's safe to forget

- The Keycloak admin password — store it in your password manager and
  don't paste it back into shell history. Day-to-day work uses your
  own user account from Step 1, not `admin`.
- Setup keys — single-use unless you tick *Reusable*; expire by
  default after seven days. Generate per machine, not per team.
- The disabled control-plane LB public interface — kubeaid-cli will
  re-disable it on every subsequent bootstrap re-run, so it's safe to
  re-enable manually for an emergency break-glass session (just remember
  the next re-run will turn it back off).

## Troubleshooting

### `netbird up` fails: "invalid JWT token audience field"

The api scope's audience mapper is set to the netbird-mgmt URL
instead of the `netbird-client` client ID. Fixed in kubeaid-cli; on a
cluster bootstrapped before the fix, retro-fit manually:

Keycloak admin → realm → Client scopes → `api` → Mappers →
"Audience for NetBird Management API" → set **Included Client Audience**
to `netbird-client`, clear **Included Custom Audience** if anything's
there → Save.

### `netbird up` fails: "Device Authorization Grant. The flow is disabled"

The OAuth 2.0 Device Authorization Grant attribute isn't set on
`netbird-client`. Fixed in kubeaid-cli; on an older cluster:

Keycloak admin → realm → Clients → `netbird-client` → Capability
config tab → tick **OAuth 2.0 Device Authorization Grant** → Save.

### `kubectl` fails: "Unauthorized"

You're authenticated but RBAC has no binding for your user / group.
Either you skipped Step 2's RBAC YAML, or your Keycloak user isn't in
the group the RoleBinding references. Easy check from a working
admin context:

```
kubectl auth can-i --list --as=oidc:<your-email>
```

### `netbird up` succeeds but the peer doesn't see other peers

NetBird's per-peer ACLs / network segmentation are stricter than just
"joined the mesh". Check NetBird Dashboard → Access Control Policies
and Network Routes.
