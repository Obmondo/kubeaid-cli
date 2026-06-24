# Hetzner bare-metal cluster — network surface

How a bare-metal kubeaid cluster's traffic divides between the Hetzner
vSwitch (private) and the public IP, and how the Cilium host firewall
(`CiliumClusterwideNetworkPolicy`) locks the public side down.

## Why the Cilium host firewall, not the Hetzner Robot firewall

The public surface is locked down with a Cilium host firewall
(`--enable-host-firewall` + a `CiliumClusterwideNetworkPolicy` selecting
all nodes via an empty `nodeSelector.matchLabels`). Earlier versions used
the Hetzner Robot API firewall. That approach had a hard **10-rule-per-direction
cap** that cannot express per-node `/32` rules for a 6-node cluster (each
node's public IP needs its own ACCEPT rule for etcd peer traffic, and a
6-node cluster's control-plane inbound ruleset exceeds 10 rules once SSH,
etcd peers, 80/443, ICMP, and return traffic are all in).

The Cilium host firewall removes the cap entirely, uses **identity-based
matching** (`remote-node`, `kube-apiserver`, `cluster`) so it needs no
per-IP enumeration and scales automatically as the cluster grows, and is
**declarative/GitOps** — the policy lives in git and ArgoCD reconciles it
rather than a kubeaid-cli imperative API call.

| | Cilium host firewall | Hetzner Robot firewall |
|---|---|---|
| GitOps | ✓ declarative, ArgoCD, self-healing | ✗ imperative (kubeaid-cli GET-diff-POST) |
| Cloud-agnostic | ✓ one policy on every node (bare-metal + HCloud + any cloud) | ✗ bare-metal Robot API only |
| Rule cap | ✓ none — identity-based | ✗ hard 10-rule-per-direction cap |
| Stateful | ✓ conntrack (no `32768-65535` return-traffic hack needed) | ✗ stateless (needs explicit return-traffic allow) |
| Kernel-resident | ✓ eBPF datapath pinned to kernel — persists across cilium-agent crash/restart | ✗ infrastructure edge — holds during boot, but imperative application |
| Fresh-node boot gap | ⚠ small gap before cilium-agent first loads (optionally covered by a thin Robot edge) | ✓ holds at the infrastructure edge from first boot |

**The "exposed in the cilium-restart window" claim in earlier versions was
wrong.** The eBPF datapath is pinned in kernel BPF maps and remains enforced
even when the cilium-agent process is down (crashed, restarting, or being
upgraded). The only genuine gap is a **fresh node boot** before the
cilium-agent first loads and pins the maps — a narrow window that can be
covered by keeping a thin Hetzner Robot firewall that allows only SSH (for
operator access during bootstrap) while the node initialises.

**Note on the Robot firewall and vSwitch traffic.** The Robot firewall does
NOT leave vSwitch / private traffic "physically untouched". In practice we
have observed the Robot firewall filtering port `10250` on the vSwitch
interface, which caused kubelet→node health check failures. This is a
Robot firewall limitation — it applies to all interfaces on the server, not
only the public NIC. The Cilium host firewall uses identity-based matching
and can be scoped precisely to `world` traffic, leaving `remote-node` /
`cluster` / `kube-apiserver` identities fully unrestricted.

**Note on NetBird overlay traffic.** NetBird peer traffic arrives over the
WireGuard overlay with overlay-CIDR source IPs, which Cilium classifies as
`world` (not cluster identity). The `fromEntities: [world]` ingress rule
must be ordered and scoped deliberately so it does not inadvertently block
the NetBird path. The kubeaid CCNP places the identity-based allow (which
is unrestricted by port) first, so cluster-internal and NetBird overlay
traffic is always permitted regardless of what the `world` rule says.

### The `controlPlaneEndpoint` and the public apiserver

The kube-apiserver is reachable on the public failover IP. CAPI's management
cluster must reach it during bootstrap (before NetBird exists), so the
apiserver is **public and auth-gated** at bootstrap time. Truly hiding the
apiserver behind a private kube-vip VIP requires that the VIP be set at
provisioning time — it is not retrofittable on a live cluster without
re-generating kubeadm certificates.

The `publicPorts` in the Cilium CCNP therefore includes `6443` only when
explicitly added by the operator (the default is `[80, 443]`). For clusters
that want the apiserver reachable from the internet, add `6443` to
`hostNetworkPolicy.publicPorts`. For clusters where the apiserver is
NetBird-only, leave it absent — the identity-based allow rule already permits
internal cluster traffic to 6443.

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

## Cilium host-firewall policy

The kubeaid chart (`argocd-helm-charts/cilium`) ships a
`CiliumClusterwideNetworkPolicy` named `kubeaid-host-firewall`, gated on
`hostNetworkPolicy.enabled: true` in the chart values. kubeaid-cli renders
this block automatically for Hetzner bare-metal clusters when
`firewall.enabled` is not explicitly `false`.

### Ingress rule order (first-match semantics)

1. **Identity allow (unrestricted)** — `fromEntities: [host, remote-node,
   health, cluster, kube-apiserver]`. Permits all cluster-internal traffic
   including node↔node etcd/kubelet, pod traffic, apiserver probes, Cilium
   health checks, and localhost. **This rule comes first** to prevent
   cluster-internal lockout.

2. **SSH from specific CIDRs** (rendered when `allowSshFrom` is non-empty)
   — `fromCIDR` rule accepting port 22/TCP from the listed sources. Bare IPs
   are normalised to `/32` at render time.

3. **World ingress** — `fromEntities: [world]` accepting the ports in
   `publicPorts` (default: `80`, `443`, `6443`) plus port `22/TCP` only when
   `allowSshFrom` is empty (empty list = SSH open to world). Also includes
   ICMP echo (`type 8, family IPv4`) for operational ping.

### Why 6443 is in the default publicPorts

The kube-apiserver on bare-metal kubeaid clusters is intentionally
**public + auth-gated**: CAPI's management cluster must reach it on the
public failover IP during bootstrap (before NetBird exists), and retrofitting
a private kube-vip VIP onto a live cluster is not feasible without re-issuing
apiserver certificates. Port 6443 is still protected by mTLS + RBAC; the host
firewall's value is denying every *other* port (scanners, uninvited ingress),
not hiding the apiserver. Locking 6443 to NetBird-only is a day-1 change
(private-VIP set at provisioning time), out of scope for the default policy.

## Override hooks (in `general.yaml`)

These live under `cloud.hetzner.bareMetal.firewall`.

| Key | Default | Purpose |
|-----|---------|---------|
| `firewall.enabled` | `true` | set `false` to opt out — the Cilium CCNP will not be rendered; the cluster relies on an upstream L3 firewall or cloud-provider security groups |
| `firewall.allowSshFrom` | `[]` | restrict inbound SSH (`22`) to these sources on every node; empty = allow from all. Each entry is an IPv4 address or CIDR (bare address ⇒ `/32`); hostnames are not accepted |

The `firewall.allowPublic` field is not rendered into the Cilium CCNP — `publicPorts`
is fixed at `[80, 443, 6443]` in the chart defaults. To expose additional ports
publicly, add them directly to `hostNetworkPolicy.publicPorts` in the chart values
overlay (`values-cilium.yaml` in the kubeaid-config repo).

## Why this matters

The split above is the load-bearing reason kubeaid clusters can have
their public IPs locked down without breaking cluster operation:

- Inter-node traffic (etcd, kubelet, pods, services) all stays on the
  vSwitch by design — the chart's `bareMetalHosts[*].privateIP` field
  populates every relevant `--advertise-address` / `--bind-address`
  / endpoint configuration. The Cilium identity rule covers this
  without per-IP enumeration.
- Pod egress to the internet routes through the host network only
  for hostNetwork pods (a tiny minority); regular pods egress via the
  CCM-managed LB or via Cilium's outbound NAT, neither of which
  touches the host's public-IP firewall directly.
- The operator-facing surfaces (`kubectl`, SSH, admin UIs) move to
  NetBird-only access — that's the actual security upgrade.
