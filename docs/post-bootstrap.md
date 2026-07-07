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
| Realm `<derived>` (e.g. `acme` for `acme.com`) | Keycloak | kubeaid-cli created it |
| OIDC client `netbird-client` (public, device flow + PKCE) | Keycloak realm | kubeaid-cli |
| OIDC client `netbird-backend` (confidential, service account) | Keycloak realm | kubeaid-cli |
| OIDC client `kubernetes-<cluster>` (public, PKCE — legacy kubelogin client, no longer used for cluster access) | Keycloak realm | kubeaid-cli |
| Client scope `api` (audience mapper → `netbird-client`) | Keycloak realm | kubeaid-cli |
| Client scope `groups` (Group Membership mapper → `groups` claim) | Keycloak realm | kubeaid-cli |
| kube-apiserver public LB | HCloud | **disabled** at finalize — reach the API through the NetBird mesh (clusterProxy) |
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
   (the derived one, e.g. `acme`). **Every step below assumes you're
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

Keycloak groups gate **who can join the NetBird mesh**: the `groups`
client scope kubeaid-cli wired up puts a `groups` claim on every token,
and NetBird Mgmt admits only tokens carrying an allowed group.

The **kube-apiserver reads no groups claim — it runs no OIDC.** kubectl
reaches it over the mesh through the netbird-operator **clusterProxy**,
which authenticates you by your NetBird identity and impersonates your
**NetBird** groups into Kubernetes RBAC (Step 4). A group still drives
kube-API access, but the one that matters there is the NetBird group
mapped in `cluster.netbird.clusterProxy.rbac` — not a Keycloak token the
apiserver validates. (NetBird can sync its groups from the Keycloak
`groups` claim, so the same names flow through.)

1. Realm sidebar → **Groups** → **Create group**.
2. Pick a name. Suggestions:
   - `mesh-users` — allowed to join the NetBird mesh.
   - `cluster-admins` — mapped (via NetBird → clusterProxy) to kube `cluster-admin`.
   - `cluster-viewers` — mapped to read-only kube access.
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

**For kube-API access**, the apiserver sees you as the **NetBird group**
the clusterProxy impersonates (Step 4). Map that group to a ClusterRole
in `cluster.netbird.clusterProxy.rbac` in `general.yaml` — the
netbird-operator renders the matching binding:

```yaml
cluster:
  netbird:
    clusterProxy:
      rbac:
        - group: cluster-admins
          clusterRole: cluster-admin
        - group: cluster-viewers
          clusterRole: view
```

(You can also hand-write a `ClusterRoleBinding` whose `subject.kind: Group`
name equals the impersonated NetBird group and manage it through
kubeaid-config — the group is the one the clusterProxy sets, not a
Keycloak JWT claim.)

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
top), and it runs no OIDC — kubectl reaches it **over the NetBird mesh
through the netbird-operator clusterProxy**, a mesh peer that proxies to
the in-cluster apiserver and impersonates your NetBird identity.

1. Make sure your laptop is on the NetBird mesh (Step 3).
2. Write the kubeconfig with NetBird's own CLI:

   ```sh
   netbird kubernetes write-kubeconfig <cluster-name>
   ```

   Its server points at the clusterProxy peer's mesh address — there is no
   kubelogin, no browser, no OIDC token. Your **mesh membership is the
   credential**; the clusterProxy impersonates your NetBird groups into
   Kubernetes RBAC (mapped in Step 2 via `cluster.netbird.clusterProxy.rbac`).

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

## Break-glass: reaching the cluster when NetBird is down

> **TL;DR** Every cluster VM is reachable from the NAT gateway over
> SSH, on the same key you used to provision it. `laptop → NAT GW →
> any private host` is the universal recovery path. It works whether
> or not k8s, NetBird, or Keycloak is healthy — you only need the NAT
> GW VM and the HCloud private network alive.

Day-to-day, `kubectl` goes through the NetBird mesh. When the mesh
breaks (NetBird Mgmt crashloop, Keycloak DB outage, expired token,
…), you fall back to one of three paths below — pick by how badly
broken things are.

### Before you start: collect these values

| Value | Where to find it |
|---|---|
| `<nat-gw-ip>` | `hcloud server list \| grep nat-g` — the public IPv4 |
| `<cp-private-ip>` | `kubectl get node -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'` or `hcloud server list \| grep control-plane` |
| `<cp-lb-id>` | `hcloud load-balancer list \| grep <cluster-name>` — first column |
| Your SSH key | The one in `general.yaml` `cloud.hetzner.sshKeyPair.privateKeyFilePath` |

Set them as env vars once so the commands below copy-paste cleanly:

```sh
export NATGW=49.13.139.134                 # <-- yours
export CP=10.0.0.4                         # <-- yours
export CPLB=6329157                        # <-- yours
export HCLOUD_TOKEN=...                    # token with read+write on LBs
```

### Path 1 — SSH straight to the control plane (fastest)

Use when you need to run a single `kubectl` command or look at logs
on the node.

```sh
ssh -J root@$NATGW root@$CP
# you're now on the CP node:
sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes
sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n netbird logs deploy/netbird-management
```

The `/etc/kubernetes/admin.conf` client cert is cluster-admin and has
no OIDC / Keycloak / NetBird dependency — works even when SSO is
completely broken.

For repeated use add this to `~/.ssh/config`:

```
Host vpn-natgw
  HostName     49.13.139.134
  User         root
  IdentityFile ~/.ssh/id_ed25519

Host vpn-*
  ProxyJump    vpn-natgw
  User         root
  IdentityFile ~/.ssh/id_ed25519
```

then `ssh vpn-cp` (where `vpn-cp` resolves to `10.0.0.4` via your
hostname map or `/etc/hosts`).

### Path 2 — `kubectl` from your laptop via SSH tunnel

Use when you want full kubectl ergonomics (autocomplete, k9s, your
local plugins) without re-enabling the public LB.

The trick: kubeadm's apiserver TLS cert always includes `kubernetes`
in its SAN list, so we point the kubeconfig at `https://kubernetes:<port>`,
map that hostname to `127.0.0.1` in `/etc/hosts`, and tunnel the port
to the apiserver. TLS verifies cleanly.

**Step 1.** One-time: add the hostname mapping. Pick any local port
that's free on your laptop — `63422` here, but anything outside the
ephemeral range works:

```sh
echo '127.0.0.1 kubernetes' | sudo tee -a /etc/hosts
export LOCAL_PORT=63422
```

**Step 2.** Copy the admin kubeconfig off the CP node — once:

```sh
scp -J root@$NATGW root@$CP:/etc/kubernetes/admin.conf ~/.kube/netbird-admin.conf
```

**Step 3.** Rewrite the server URL to `https://kubernetes:$LOCAL_PORT`.
The original file has `server: https://10.0.0.3:6443` (or the public
LB IP) — swap it out:

```sh
sed -i "s|server: https://.*:6443|server: https://kubernetes:$LOCAL_PORT|" \
  ~/.kube/netbird-admin.conf
```

**Step 4.** Open a backgrounded SSH tunnel. `-f` backgrounds after
auth, `-N` skips opening a shell — the tunnel stays up until you kill
the ssh PID.

```sh
ssh -fN -L $LOCAL_PORT:127.0.0.1:6443 -J root@$NATGW root@$CP
```

**Step 5.** `kubectl` just works:

```sh
export KUBECONFIG=~/.kube/netbird-admin.conf
kubectl get nodes
kubectl -n netbird logs deploy/netbird-management
```

Or alias it so you don't have to set `KUBECONFIG` every shell:

```sh
alias knb='kubectl --kubeconfig ~/.kube/netbird-admin.conf'
knb get nodes
```

**To stop the tunnel:**

```sh
pkill -f "ssh -fN -L $LOCAL_PORT"
```

### Path 3 — re-enable the public LB interface (cluster on fire)

Use when both other paths are too slow and you want the kube-apiserver
on the open internet for a few minutes. Reverses kubeaid-cli's
`DisableControlPlaneLBPublicInterface`.

```sh
hcloud load-balancer enable-public-interface $CPLB
# ...incident work — your normal kubeconfig works again...
hcloud load-balancer disable-public-interface $CPLB
```

> **Don't forget the second command.** If you leave the public
> interface enabled, the cluster's API is on the open internet until
> the next `kubeaid-cli bootstrap` re-run disables it again.

### Multi-node and partial failures

Same shape scales: every HCloud server in the private network is
reachable from the NAT GW with the same key. Worker died, etcd
corrupted, kubelet wedged on a single node, network split — none of
that affects the SSH path.

For specific failure modes:

- **Single worker node dead, control plane fine**: SSH-jump to the
  CP node, run `kubectl drain` / `kubectl delete node` as usual via
  `admin.conf`. CAPI / cluster-autoscaler will reconcile a
  replacement.
- **Control plane node dead, HA enabled**: SSH-jump to a surviving
  CP node and use its `admin.conf`. Treat etcd member loss with the
  usual [etcd disaster recovery procedure](https://etcd.io/docs/latest/op-guide/recovery/).
- **Whole cluster gone, kubeaid-config still intact**: re-run
  `kubeaid-cli bootstrap` against the same `general.yaml` — CAPI's
  pivot + the rest of the bootstrap flow rebuilds the cluster against
  the existing kubeaid-config repo. The HCloud LB and network are
  reused if they still exist, recreated otherwise.

### What to keep handy

The break-glass path needs three artifacts that you should have
stored *outside* the cluster (a password manager, an offline copy,
a separate secrets store — wherever your team keeps DR material):

1. The SSH private key kubeaid-cli rendered for HCloud server access.
2. A copy of `/etc/kubernetes/admin.conf` from a healthy CP node,
   plus the cluster CA bundle. Refresh after any control-plane roll
   (the cert SANs change).
3. The Keycloak admin password — only relevant if Keycloak itself is
   the thing that's down, but easiest to keep alongside the others.

Without these, the break-glass path still works (you can re-derive
admin.conf via `kubeadm` on the node), it's just slower.

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

Your NetBird identity has no RBAC binding. Either you skipped Step 2's
`clusterProxy.rbac` mapping, or your NetBird group isn't bound to a
ClusterRole. Check from a working admin context using the impersonated
group (there is no `oidc:` identity — the apiserver runs no OIDC):

```
kubectl auth can-i --list --as-group=<your-netbird-group>
```

### `netbird up` succeeds but the peer doesn't see other peers

NetBird's per-peer ACLs / network segmentation are stricter than just
"joined the mesh". Check NetBird Dashboard → Access Control Policies
and Network Routes.
