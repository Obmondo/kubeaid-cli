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

| Port / Protocol | Rule | Why |
|-----------------|------|-----|
| `6443/tcp` | **DENY** | kube-apiserver reached via NetBird mesh only |
| `22/tcp` | **DENY** | SSH reached via NetBird mesh only |
| `80/tcp`, `443/tcp` | ALLOW | Traefik dual-IngressClass decides per-route (public `web` / `websecure` vs internal `netbird-web` / `netbird-websecure`) |
| ICMP echo | ALLOW | operational ping for monitoring / debugging |
| return traffic (high ports 32768–65535) | ALLOW | stateless firewall needs explicit allow for established replies |

### Public IP — OUTBOUND

| Phase | Rule | Why |
|-------|------|-----|
| **Bootstrap window** (cloud-init's apt / wget chain) | full allow | tightening earlier breaks the install |
| **Post-bootstrap** (NetBird agent up + workload-cluster ArgoCD synced) | allow `53/tcp+udp`, `80/tcp`, `443/tcp`; deny rest | DNS, apt mirrors, container registries, GitHub releases for `kubeaid-storagectl-on-OS-upgrade`, etc. |

### NetBird-side exposure (no Robot firewall involvement)

| Service | NetBird-only access |
|---------|---------------------|
| kube-apiserver | `6443/tcp` via node's NetBird IP |
| SSH | `22/tcp` via node's NetBird IP |
| Admin UIs (ArgoCD, Keycloak admin, Grafana) | bound to the `netbird-web` / `netbird-websecure` IngressClass |

## Open design decision: `controlPlaneEndpoint`

Blocking inbound `:6443` on the public IP **requires** picking one path.
This decision gates implementation of `EnsureRobotFirewall` —
specifically what the inbound rule for `:6443` looks like.

| Option | What changes | Trade-off |
|--------|--------------|-----------|
| **A. vSwitch-routable endpoint** | `controlPlaneEndpoint` → master's `10.0.1.1` (or a VRRP IP on the vSwitch); operators reach API via NetBird only | Clean. Requires kubeaid-cli + operator's `kubectl` to be on the NetBird mesh — no fallback |
| **B. Source-IP allow-list on public `:6443`** | Keep failover-IP-fronted `:6443` publicly reachable; allow only operator NetBird egress + kubeaid-cli's host IP | Quicker to implement. Leaves a (small) public attack surface; allow-list needs maintenance as operator IPs change |

## Override hooks (planned, in `general.yaml`)

`EnsureRobotFirewall` will read these from `cloud.hetzner.bareMetal.firewall`:

| Key | Purpose |
|-----|---------|
| `firewall.allowPublic` | list of `{port, protocol}` items appended to the inbound ALLOW rules (e.g. `25/tcp` for SMTP, `5432/tcp` for a customer-exposed Postgres) |
| `firewall.enabled: false` | opt out entirely (cluster runs an upstream L3 firewall appliance) |

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
