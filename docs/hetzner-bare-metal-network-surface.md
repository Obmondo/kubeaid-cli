# Hetzner bare-metal cluster — network surface

How a bare-metal kubeaid cluster's traffic divides between the Hetzner
vSwitch (private) and the public IP, and the Hetzner Robot stateless
firewall ruleset that locks the public side down.

Status: design captured here; implementation tracked in
[`docs/TODO.md`](TODO.md) under "Apply Hetzner Robot stateless firewall
on bare-metal nodes". `EnsureRobotFirewall` is not yet wired into the
prereq-infra phase.

## Why the Robot firewall, not a Cilium host network policy

The public surface could also be locked down with a Cilium host network
policy (`--enable-host-firewall` + a `CiliumClusterwideNetworkPolicy`
selecting the host) — which would be GitOps-managed and cloud-agnostic. We
use the Hetzner Robot firewall instead. The trade-off, for this specific
goal (deny public `22`/`6443`, keep `80`/`443`, operator access via NetBird
only):

| | Hetzner Robot firewall | Cilium host netpol |
|---|---|---|
| GitOps | ✗ imperative (kubeaid-cli GET-diff-POST — declarative in code, not ArgoCD-reconciled) | ✓ declarative, ArgoCD, self-healing |
| Cloud-agnostic | ✗ bare-metal Robot API only (HCloud nodes need a separate firewall) | ✓ one policy on every node (bare-metal + HCloud + any cloud) |
| Scoping safety | ✓ touches only the public IP — the vSwitch fabric (etcd/kubelet/pods) and the NetBird interface are physically untouched | ⚠ sees all host traffic; a wrong host policy has cluster-wide blast radius |
| Always-on | ✓ holds during boot and even if Cilium is down | ✗ enforces only while Cilium is up → SSH exposed in the boot / cilium-restart window |
| State | stateless (needs the explicit `32768-65535` return-traffic allow) | ✓ stateful (conntrack) |

The deciding factor is the NetBird interaction. NetBird peer traffic arrives
over the WireGuard overlay with overlay-CIDR source IPs, which Cilium
classifies as `world` (not cluster identity). A naive `ingressDeny` of
`world → host:22/6443` would therefore also drop the NetBird path we are
trying to keep open — the Cilium policy would need an explicit allow for the
NetBird CIDR, correctly ordered ahead of the deny, plus correct host-traffic
allow-listing, or it locks the node out cluster-wide. The Robot firewall
sidesteps this entirely: the public IP and the NetBird interface are
physically separate paths, so a public-IP rule cannot affect NetBird or
vSwitch traffic.

So for bare-metal public-IP lockdown the Robot firewall is the safer
primary — purpose-built to filter the public NIC, unable to break
cluster-internal or NetBird traffic, and enforced at the infrastructure edge
even before Cilium is up. The GitOps loss is contained: `EnsureRobotFirewall`
reconciles idempotently (GET, diff, POST); it is just driven by kubeaid-cli
rather than ArgoCD.

A Cilium host policy is the better choice when a single GitOps-managed policy
across hybrid HCloud + bare-metal clusters matters more than the boot-window
perimeter. The two are complementary: a thin always-on Robot edge plus Cilium
for richer in-cluster / L7 policy is a reasonable defense-in-depth target.

## Traffic split: vSwitch (private) vs public IP

### Stays on vSwitch — never touches the public IP

The Hetzner vSwitch is an L2 segment between the bare-metal nodes (and
to the HCloud network for hybrid mode). Traffic on it never leaves
Hetzner's datacenter network. Blocking the public IP doesn't affect
anything in this table.

| Traffic | Source IP | Destination IP | Why it matters |
|---------|-----------|----------------|----------------|
| etcd peer-to-peer | `10.0.1.x` (vSwitch) | other masters' `10.0.1.x` | quorum + state replication |
| kubelet → kube-apiserver | `10.0.1.x` | `controlPlaneEndpoint` | node health / pod status |
| pod-to-pod (Cilium native routing) | node `10.0.1.x` + pod CIDR | other nodes' `10.0.1.x` + pod CIDR | workload traffic |
| CCM-managed internal Services / LB | `10.0.1.x` | `10.0.1.x` | intra-cluster service mesh |

### Must touch the public IP today

| Traffic | Why it can't (yet) move off public |
|---------|-----------------------------------|
| `controlPlaneEndpoint: <failover-IP>:6443` | failover IP delivered to master's public NIC; kernel routes `:6443` there |
| cloud-init `apt-get` + `wget` (during install) | egress to apt mirrors / GitHub releases / container registries |
| Operator's `kubectl` / `argocd` from laptop | public `:6443` today; goal is NetBird-only |
| Public-facing Traefik (customer routes) | `web` / `websecure` IngressClass on public `:80` / `:443` |

## Hetzner Robot stateless firewall — port rules

### Public IP — INBOUND (default deny)

Control-plane and worker nodes get different rulesets, because public ingress
enters only through the control-plane failover IP.

**Control-plane nodes** (hold the failover IP — the cluster's single public ingress):

| Port / Protocol | Rule | Why |
|-----------------|------|-----|
| `22/tcp` | ALLOW from `allowSshFrom`, else from all | SSH — the hosts are not NetBird peers, so there is no mesh path to host SSH; restrict by source IP instead. Empty `allowSshFrom` = open |
| `6443/tcp` | **DENY** | kube-apiserver reached over the NetBird operator (which exposes the Service onto the mesh), never publicly |
| `80/tcp`, `443/tcp` | ALLOW to the **failover IP** | Traefik public ingress — `dst_ip`-scoped to the failover IP, so a CP node's own main IP serves nothing public but SSH |
| `allowPublic` ports | ALLOW to the **failover IP** | extra public services (e.g. `5432/tcp`), same `dst_ip` scoping |
| ICMP echo | ALLOW | operational ping |
| return traffic (32768–65535) | ALLOW | stateless firewall needs an explicit allow for replies |

**Worker nodes** (serve no public traffic — it all enters via the failover IP):

| Port / Protocol | Rule | Why |
|-----------------|------|-----|
| `22/tcp` | ALLOW from `allowSshFrom`, else from all | admin SSH only |
| ICMP echo | ALLOW | operational ping |
| return traffic (32768–65535) | ALLOW | replies to node-initiated egress |
| everything else | **DENY** (implicit) | no public service surface on workers |

To put a service on its own public IP instead of the failover IP, order an
additional Hetzner IP and announce it via MetalLB L2 on a node; that node then
needs the matching port opened.

### Public IP — OUTBOUND

| Phase | Rule | Why |
|-------|------|-----|
| **Bootstrap window** (cloud-init's apt / wget chain) | full allow | tightening earlier breaks the install |
| **Post-bootstrap** (NetBird agent up + workload-cluster ArgoCD synced) | allow `53/tcp+udp`, `80/tcp`, `443/tcp`; deny rest | DNS, apt mirrors, container registries, GitHub releases for `kubeaid-storagectl-on-OS-upgrade`, etc. |

### NetBird-side exposure (via the NetBird operator, not host peers)

The bare-metal hosts are **not** NetBird peers — only the NetBird Kubernetes
operator runs, exposing in-cluster **Services** onto the mesh. So:

| Service | NetBird access |
|---------|----------------|
| kube-apiserver | the operator exposes the `kubernetes` Service on the mesh (covered by the `kubernetes.<netbird-dns-zone>` cert-SAN) |
| Admin UIs (ArgoCD, Keycloak admin, Grafana) | bound to the `netbird-web` / `netbird-websecure` IngressClass |
| SSH | **not** on the mesh — there is no host peer; restrict public `22/tcp` via `allowSshFrom`, or reach nodes through a bastion on the vSwitch |

## `controlPlaneEndpoint` — resolved

`6443` on the public IP is **denied**. Operators reach the apiserver over the
NetBird operator, which exposes the in-cluster `kubernetes` Service onto the mesh
(the `kubernetes.<netbird-dns-zone>` cert-SAN covers the name). The failover IP
keeps serving public `80/443` (Traefik); it simply no longer fronts a public
apiserver. Internal kubelet → apiserver traffic is unaffected — it rides the
vSwitch, which the public firewall never touches.

## Override hooks (in `general.yaml`)

These live under `cloud.hetzner.bareMetal.firewall`. The config surface and the
ruleset logic are implemented — `config.FirewallConfig` (parsed + validated),
`hetzner.ControlPlaneIngressRuleset` / `hetzner.WorkerIngressRuleset` (the
per-role builders), and `hetzner.FirewallEnabled` (the opt-out check). The
`tools/applyfirewall` command applies them against the Robot API directly; the
in-CLI provisioning-phase wiring is the remaining follow-up.

| Key | Default | Purpose |
|-----|---------|---------|
| `firewall.enabled` | `true` | set `false` to opt out entirely (cluster runs an upstream L3 firewall appliance) |
| `firewall.allowSshFrom` | `[]` | restrict inbound SSH (`22`) to these sources on every node; empty = allow from all. Each entry is an IPv4 address or CIDR (bare address ⇒ `/32`); hostnames are not accepted |
| `firewall.allowPublic` | `[]` | `{port, protocol}` items opened on the public ingress IP (control-plane failover IP) alongside `80/443` (e.g. `25/tcp` SMTP, `5432/tcp` Postgres). Control-plane only — workers expose no public ports. `6443` is always denied, so this cannot re-open the apiserver |

## Why this matters

The split above is the load-bearing reason kubeaid clusters can have
their public IPs locked down without breaking cluster operation:

- Inter-node traffic (etcd, kubelet, pods, services) all stays on the
  vSwitch by design — the chart's `bareMetalHosts[*].privateIP` field
  populates every relevant `--advertise-address` / `--bind-address`
  / endpoint configuration. The Robot firewall affects none of it.
- Pod egress to the internet routes through the host network only
  for hostNetwork pods (a tiny minority); regular pods egress via the
  CCM-managed LB or via Cilium's outbound NAT, neither of which
  touches the host's public-IP firewall directly.
- The operator-facing surfaces (`kubectl`, SSH, admin UIs) move to
  NetBird-only access — that's the actual security upgrade.

The Robot firewall doesn't make the cluster more isolated *internally*;
it removes the *operator-facing* public attack surface (port 22 and
6443) while keeping the customer-facing surface (80, 443) intact.
