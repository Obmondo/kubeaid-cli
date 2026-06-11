# Docs TODO

## Troubleshooting guide

A `docs/troubleshooting.md` should collect the recovery paths for
recurring bootstrap failures so operators don't have to re-discover
them from chat logs. Initial topics:

### Hetzner

- **CAX server type not available in datacenter** (`error during placement (resource_unavailable, ...)`)
  - How to spot in the live status table
  - When it's transient (Hetzner capacity) vs misconfig (wrong type for location)
  - Workaround: update `Cloud.Hetzner.ControlPlane.HCloud.MachineType` and re-run

- **`HCloudMachineTemplate.Spec is immutable` on ArgoCD sync after config change**
  - Why: CAPH treats template specs as immutable
  - Pre-bootstrap recovery: `kubectl delete machine -l cluster.x-k8s.io/cluster-name=<cluster>` then `kubectl delete hcloudmachinetemplate <name>` then re-run
  - Post-bootstrap: use `kubeaid-cli cluster upgrade` instead
  - Finalizer hangs: `kubectl patch machine <name> --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'` ONLY when the cloud-side server is known not to exist

- **Wait stuck on `NodeHealthy: Waiting for Cluster control plane to be initialized`**
  - Phase=Provisioned + InfrastructureReady=true means the server is up but kubeadm hasn't completed on the node
  - VPN cluster has no public IPv4 on the control plane — SSH via the NAT gateway:
    ```bash
    ssh -J root@<nat-gateway-public-ip> root@<cp-private-ip>
    cloud-init status
    cat /var/log/cloud-init-output.log | tail -100
    journalctl -u kubelet -n 100 --no-pager
    ls /etc/kubernetes/    # admin.conf only present once kubeadm init finishes
    crictl ps -a           # are kube-apiserver / etcd static pods running?
    ```
  - Common failure modes: image pull stalled, etcd port bind failure, cert SAN mismatch

- **Manual NAT gateway deletion from HCloud console**
  - kubeaid-cli will recreate on re-run, but the deletion-protection toggle needs both `delete` and `rebuild` set together (per Hetzner API)
  - If kubeaid-cli predates the fix, manually toggle protection off before delete

### Sealed Secrets

- Sealed Secrets controller in management vs main cluster has different keys — re-encryption happens during `SetupCluster` on main cluster
- If a re-run hits "sealed secrets values mismatch", check the SHA-256 cache header `# kubeaid-sha256:` in each sealed-secret file under the kubeaid-config fork

### ArgoCD

- "Manifest generation error (cached)" — repo-server caches manifests; force a fresh comparison via:
  `kubectl annotate app <name> argocd.argoproj.io/refresh=hard --overwrite`

## Pending feature work

### Wire netbird-operator config (managementURL + API token)

`netBirdOperatorEnabled()` in `pkg/core/templates.go` now renders
the operator on both workload-with-Keycloak and VPN clusters (the
VPN-cluster install was the immediate fix that landed alongside
this TODO). But the values overlay
(`values-netbird-operator.yaml.tmpl`) is still empty — the operator
pod runs with chart defaults: no `managementURL`, no
`netbirdAPI.keyFromSecret`, no `cluster.name`. It can't talk to
NetBird Mgmt's API, so NBSetupKey / NBPolicy / NetworkRouter /
NetworkResource CRs created against it sit pending.

To make the operator actually usable, the v0.4.1 chart needs:

```yaml
managementURL: https://netbird.<parent-or-self-dns>
cluster:
  name: <cluster name>
  dns: svc.cluster.local
netbirdAPI:
  keyFromSecret:
    name: ...   # a SealedSecret kubeaid-cli renders
    key: ...
```

Open design decisions (deferred from a hands-on discussion — see the
chat that led to this TODO entry):

1. **Config shape in general.yaml.** Likely a new explicit
   `cluster.netbird: { mgmtURL, apiKeySecret: {name, key} }` block.
   Decoupling from `cluster.keycloak` matters for operators who
   bring their own NetBird without a parent VPN's Keycloak.

2. **NetBird API token source.** Three reasonable options:
   - Operator-provided, prompt for an existing Secret name/key.
     Mirrors the existing operator-managed Keycloak admin creds.
   - Auto-generated for managed-Keycloak VPN clusters: after
     NetBird Mgmt is healthy, kubeaid-cli signs in via a service
     user, mints a PAT through NetBird's API, stamps it into a
     SealedSecret. Needs a new `pkg/netbird/` Go client (similar
     shape to `pkg/keycloak/`).
   - Skip the API key on the first cut — render the operator pod
     without it, document the manual-wire-up. Smallest scope;
     CRDs and pod ship, operator finishes the wiring later.

3. **VPN cluster scope.** If we install the operator there with a
   self-referential `managementURL: https://netbird.<own-vpn-dns>`,
   it unlocks `NetworkRouter` / `NBRoutingPeer` CRs that close the
   "no peer in the HCloud private network advertises 10.0.0.0/24"
   gap from `docs/post-bootstrap.md`'s break-glass discussion —
   kubectl-over-mesh would work without ssh-jumping through NAT GW.

4. **Prompt UX.** Workload-cluster prompt currently derives the
   NetBird Mgmt URL by string-munging the Keycloak DNS
   (`keycloak.X` → `netbird.X` in `pkg/config/prompt/prompt.go:319`)
   instead of asking. Add an explicit "do you have a NetBird
   server?" group, similar in shape to `runWorkloadKeycloakForm`.

When picking this up: pair with the v0.4.1 chart bump
([kubeaid commit `7d6834816`](https://gitea.obmondo.com/Obmondo/kubeaid))
to get the new `NetworkRouter` / `NetworkResource` / `SidecarProfile`
CRDs while keeping the legacy `NB*` CRDs available for in-flight
configs.

#### Status update (2026-06-08)

- `awaitNetBirdOperatorToken` now pauses bootstrap until the
  operator pastes a service-user PAT and creates
  `netbird/netbird-mgmt-api-key`, so a fresh bootstrap doesn't lock
  the cluster on the operator's `failurePolicy: Fail` webhook with a
  missing-Secret deployment.
- **NetBird Mgmt rejects Keycloak-issued JWTs** — the auto-mint path
  (option 2 above) is *not directly buildable*. Test: a JWT minted
  via `client_credentials` against `netbird-backend` is rejected at
  `/api/users` with 401 because Mgmt looks up the JWT's `sub` claim
  in its internal user DB, and service-account users have never
  logged in via the dashboard. Either upstream Mgmt needs an
  `azp`-based admin path or kubeaid-cli needs to pre-seed the user
  record (no obvious way to do that without a human session).

### NetBird operator PAT rotation

NetBird user PATs cap at 180 days; service-user PATs may allow
longer but no official no-expiry. Cluster ops on a 180-day
rotation isn't acceptable long-term. Two options:

1. **In-cluster CronJob** — uses the current PAT to mint a new one
   via NetBird Mgmt's `POST /api/users/<id>/tokens`, patches the
   `netbird-mgmt-api-key` Secret, rolls the operator. Schedule
   every ~5 months. Self-perpetuating once seeded.

2. **Upstream patch** — allow `--no-expiry` (or a much longer cap,
   e.g. 5y) on service-user PATs only. Smaller Mgmt-side change;
   maintainers likely receptive given the automation-friendly
   service-user framing.

Pair (1) into the operator-token gate's chart overlay; pursue (2)
in parallel and drop the CronJob when it lands.

### Cilium components must reach kube-apiserver without DNS

Bug seen on the `netbird-obmondo-com` bootstrap: after
`DisableControlPlaneLBPublicInterface` runs, every Cilium component
(both operator Deployment and agent DaemonSet) crashloops because
its `KUBERNETES_SERVICE_HOST` is the public hostname
(`api.vpn.<cluster>`), which resolves to the now-blackholed public
LB IP via the host's `/etc/resolv.conf` (hostNetwork pods skip
CoreDNS — `dnsPolicy: ClusterFirst` is silently downgraded to
`Default`). Go's HTTP client tries the first IP in the resolution
list and hangs on the TCP blackhole for the full kernel retry
window (~75–127s), so fallback to the LB private IP never fires
within the operator's startup deadline.

Two layered fixes:

1. **In-cluster `KUBERNETES_SERVICE_HOST`** — overlay the cilium
   values to pin `k8sServiceHost` to `{{ .ControlPlaneLBPrivateIP }}`
   instead of the hostname. Bypasses DNS entirely. Simplest, but
   loses the symbolic reference to the cluster endpoint.

2. **`hostAliases` via upstream chart PR** — keep
   `k8sServiceHost: api.vpn.<cluster>` and inject a hostAliases
   entry mapping that hostname to the LB private IP, so
   kubelet-managed `/etc/hosts` resolves it correctly even for
   hostNetwork pods. Requires upstream Cilium PR for
   `extraHostAliases` / `operator.extraHostAliases` values (draft
   prepared in this session's chat, not yet filed).

Pursue (2) upstream; ship (1) as the immediate kubeaid-cli fix in
`values-cilium.yaml.tmpl`. Drop (1) when (2) is released.

### CoreDNS hosts block leaves the disabled public IP

`pkg/core/templates/k8s-configs/coredns.configmap.yaml.tmpl`
emits both the LB's bootstrap public IP and the steady-state
private IP, with the public IP first. After
`DisableControlPlaneLBPublicInterface` runs, the public entry
points at a blackhole, but stays in the ConfigMap forever — every
pod looking up `api.vpn.<cluster>` via CoreDNS still gets the dead
address as the preferred answer.

Two fixes:

- **Reorder** — emit the private IP first. Clients hit it
  instantly; public stays as harmless fallback.
- **Conditional emit** — only include the public IP while the LB
  public interface is still up. After
  `DisableControlPlaneLBPublicInterface` runs, re-render and push
  the ConfigMap without the public line. Cleaner long-term.

Reorder is a one-line change; conditional emit needs the disable
step to trigger a re-render. Land reorder first; build conditional
emit as part of the same flow that re-disables on rerun.

### Default the netbird-operator webhook to `failurePolicy: Ignore`

The upstream chart ships `MutatingWebhookConfiguration` with
`failurePolicy: Fail`. On a single-CP cluster where the operator
itself crashloops (missing API key, cert-manager not yet ready,
etc.), this blocks every cluster-wide Pod create — including the
operator's own rollouts, making it almost impossible to recover
without SSH-into-the-node patches. Overlay
`webhook.failurePolicy: Ignore` in
`values-netbird-operator.yaml.tmpl` so the cluster degrades
gracefully when the operator is unhealthy. Optional sidecar
injection is the worst-case loss; an unwedged cluster is the win.

Belongs with the broader operator-config TODO above, but worth
shipping standalone if that wider work slips.

### Hard-fail `kubeaid-cli login` on a NetBird mesh mismatch

`pickCluster` (`cmd/kubeaid-cli/login/login.go`) compares the local
NetBird daemon's management URL against `global.NetBird.ManagementURL`
from klist's `global.yaml`, but a mismatch only emits a `slog.Warn` —
login then proceeds against whatever mesh the daemon is on, leaving
the user with an easy-to-miss warning and a wrong or empty cluster
list.

The `bootstrap` pre-flight (`requireOperatorOnNetBird`) was made to
hard-fail on exactly this mismatch — see `fix(netbird): verify the
bootstrap operator is on the right mesh`. `login` should get the same
treatment: turn the warn-only branch in `pickCluster` into a returned
error that tells the user to `netbird up --management-url` against the
right server first.

Deferred rather than bundled into the bootstrap fix because `login`
keys off klist's `global.yaml`, not `general.yaml` — a different
config surface worth handling on its own.

### Detect `make build` dev versions in `storagectlVersion`

`Makefile:1` injects `VERSION = $(git describe --tags --always --dirty)`
into `cmd/kubeaid-core/root/version.Version`, so a local `make build`
run produces a string like `v0.23.0-54-g0d24247-dirty`. The gate in
`pkg/core/templates.go:162` (`storagectlVersion`) only treats `""` and
`"dev"` as dev, so any Makefile-built kubeaid-cli pins that describe
string into `global.kubeaidStoragectl.version` of the rendered
`values-capi-cluster.yaml`. Result: every commit on main produces a
noise diff + PR in kubeaid-config on every bootstrap run, and the
chart's `latest` fallback (intended for dev) never fires.

Extend the gate to recognise git-describe dev markers:

- suffix `-dirty` → dev (return `""`)
- segment `-g<hex>` (post-tag git-describe form, with or without
  `-dirty`) → dev

Release tags from goreleaser (`{{ .Tag }}` → `v0.23.0`, `v0.23.0-rc.1`)
keep passing through verbatim. Extend `TestStoragectlVersion` with the
new dev cases (`v0.23.0-dirty`, `v0.23.0-54-g0d24247`,
`v0.23.0-54-g0d24247-dirty`) so the regex can't drift silently.

### Pre-flight ArgoCD-rendered Helm values against the chart's schema

A broader-scope follow-up to the Hetzner bare-metal regions fix: a
local pre-flight that runs `helm template --validate` (or
`kubeconform`, or `jsonschema`) against the rendered
`values-capi-cluster.yaml` before kubeaid-cli pushes the kubeaid-config
PR. The bare-metal regions case was caught the hard way (ArgoCD sync
failure) because `go-playground/validator` only checks slice
non-nil-ness on `required` — a Helm schema's `minItems`, `pattern`,
or other JSONSchema constraints aren't enforced on the Go side. A
pre-flight surfaces the failure as a clean field-level error from
kubeaid-cli with the offending path, same shape as the parser's
existing `validate` errors. Defer until we hit the next case from a
different field; the regions one is fixed at source.

### Apply Hetzner Robot stateless firewall on bare-metal nodes

Bare-metal nodes need a public v4 attached during bootstrap so cloud-
init can `apt-get update / install` (cloud-init pulls cloud-init +
apparmor in the post-install script, then containerd + runc + kubeadm
in preKubeadm). The original design intent was private-IP-only via
NetBird, but stripping the public IP outright breaks the egress those
APT pulls need.

Mitigation: keep the public IP attached but apply Hetzner Robot's
per-server stateless firewall to block all ingress + all egress
*except* DNS (53/udp + 53/tcp) and the return-traffic high-port range
that stateless rules need to allow explicitly. Outbound APT mirror
traffic stays open during the install window, then the firewall is
locked down once the node is up on NetBird and direct internet egress
isn't needed.

Implementation shape (mirrors `CreateHetznerBareMetalSSHKey`):

- New method on the Hetzner client: `EnsureRobotFirewall(serverID,
  rules)` that PUTs `/firewall/<serverID>` with the templated rules.
  Idempotent — Robot returns the current config; only re-PUT if it
  differs.
- Called from `ProvisionPrerequisiteInfrastructure` after the
  SSH-reachable wait passes, before `GenerateStoragePlans` runs.
- Rules template lives in `pkg/cloud/hetzner/firewall.go` (or chart
  values if operators need to tweak per-cluster). Default ruleset:
  outbound 53/udp+tcp to anywhere, inbound established (high ports
  32768+) for return traffic, deny rest. Override hook for operators
  who need to expose additional ports (e.g. failover-IP-fronted
  apiserver before NetBird takes over).
- Timing of the lock-down: open during cloud-init, tightened once
  NetBird agent is up on the node. Easiest UX: apply the strict rules
  from kubeaid-cli only after the workload-cluster ArgoCD apps have
  synced and NetBird peer is registered. Pre-NetBird the rules stay
  open for egress to the world.

Open questions before implementation:

- Apply pre-NetBird or post-NetBird? Pre is safer (smaller exposed
  window) but breaks the APT install if rules are too strict; post is
  simpler but leaves the node with full public egress for the install
  window.
- Per-server vs per-cluster API call shape: Robot's firewall API is
  per-server. Looping over each host is fine for the kbm fleet size,
  but rate limits start mattering on bigger clusters. Worth
  benchmarking before committing.
- What about the failover IP / control-plane endpoint? The Robot
  firewall rules apply to the server's main IP; the failover IP gets
  delivered to whichever server currently holds it, and its inbound
  traffic is subject to the receiving server's firewall. Rules must
  allow inbound 6443 from the operator's egress IPs (or accept that
  the operator reaches the API via NetBird only).

Filed by request — keep public IP attached during bootstrap, tighten
afterwards via Hetzner Robot firewall, default-deny except DNS.

Concrete ingress / egress contract (operator-confirmed):

  Public IP — INBOUND (default deny, explicit allow per port):
    - 6443/tcp  → DENY. kube-apiserver MUST NOT be reachable via the
                  public IP. Operators, kubectl, CI runners, GitOps
                  controllers, etc. all reach the API through the
                  NetBird mesh. (Failover-IP-fronted apiserver covered
                  in the existing question above — same rule, just
                  applied to the holder's main IP.)
    - 22/tcp    → DENY. SSH reachable only via NetBird; the operator
                  hops onto the mesh first and then SSHes against the
                  node's NetBird-assigned IP. Eliminates the public
                  attack surface on credentials.
    - 80/tcp,   → ALLOW selectively. Traefik runs in a dual-Ingress
      443/tcp     shape: one IngressClass for customer-facing routes
                  (`web`/`websecure` accept public traffic) and a
                  second for internal-only routes
                  (`netbird-web`/`netbird-websecure` only listen on
                  the node's NetBird interface). The Robot firewall
                  allows 80+443 unconditionally on the public IP —
                  Traefik itself decides per-Ingress whether to
                  answer, via the IngressClass + the
                  `traefik.ingress.kubernetes.io/router.entrypoints`
                  annotation. So the firewall rule is broad; the
                  per-route policy lives in the Ingress objects.
    - ICMP echo → ALLOW (operational ping for monitoring / debugging).
    - established/related return traffic → ALLOW (stateless: explicit
      allow on the high-port range 32768-65535).

  Public IP — OUTBOUND (default allow, with a tightening lever):
    - During bootstrap window (cloud-init's apt-get + wget chain):
      full egress allow. Tightening earlier breaks the install.
    - After bootstrap completes (NetBird agent up + workload-cluster
      ArgoCD synced): tighten to:
        - 53/tcp + 53/udp → ALLOW (DNS)
        - 80/tcp + 443/tcp → ALLOW (apt mirrors, container registries,
          GitHub releases for kubeaid-storagectl-on-OS-upgrade, etc.)
        - All else → DENY.
      Egress lock-down timing handled by the same kubeaid-cli phase
      that flips the inbound deny — see open question above on
      pre-NetBird vs post-NetBird application.

NetBird-side exposure (no Robot firewall involvement):
  - 6443/tcp via the node's NetBird IP → operator's kubectl / CI.
  - 22/tcp via the node's NetBird IP → operator's SSH.
  - Internal-only Ingress (`netbird-web` / `netbird-websecure`) →
    admin endpoints, ArgoCD UI, Keycloak admin console, kube-prom
    Grafana, etc. These never touch the public Traefik entrypoint
    because the Ingress is bound to the internal IngressClass.

Implementation knobs in `EnsureRobotFirewall`:
  - Default ruleset above is the baked-in template.
  - Override hook for clusters that need a port exposed publicly
    (e.g. a customer self-hosting needs 25/tcp for SMTP, or 5432/tcp
    for a Postgres they want internet-reachable). Override lives in
    general.yaml under `cloud.hetzner.bareMetal.firewall.allowPublic`
    as a list of `{port, protocol}` items appended to the ALLOW
    rules.
  - Operator can opt out entirely via
    `cloud.hetzner.bareMetal.firewall.enabled: false` — for clusters
    where the operator's running a separate L3 firewall appliance
    upstream.
