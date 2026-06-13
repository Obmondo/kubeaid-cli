# NetBird mesh DNS zone + apiserver cert SANs

Date: 2026-06-13
Status: Approved → implementing

## Problem

kube-apiserver's serving cert only carries the control-plane endpoint host plus
kubeadm's defaults (node IPs, `10.96.0.1`, `kubernetes.default`, …). A client
reaching the apiserver under a NetBird-mesh hostname hits an x509 name mismatch
— which is why kbm had to put a raw IP in its klist `server:`. We want:

1. a predictable, per-cluster mesh name in **every** cluster's cert by default;
2. an operator escape hatch (`extraCertSANs`) for additional names that also
   works on **bare-metal** (today it only works on the hcloud LoadBalancer path).

## Design

### NetBird DNS zone — config + prompt

- New field **`cluster.netbird.dnsZone`**. When empty, defaults (computed) to
  **`<cluster.name>.local`** — unique per cluster (e.g. `kbm-obmondo-com.local`).
  Because it's derived from the cluster name, the default lives in code (parser),
  not a static struct tag.
- **`cluster.netbird` becomes valid for both `cluster.type: vpn` and
  `workload`.** Its `dns` / `stun` / `turn` sub-fields stay required only for
  `type=vpn` (the cluster that *hosts* NetBird Mgmt). So **`cluster.type` is the
  gate** — `vpn` = host + zone, `workload` = join + zone — with no extra
  "is netbird enabled?" boolean.
- The prompt asks for the **DNS zone** (default `<cluster.name>.local`) for both
  types; `vpn` additionally collects the existing Mgmt-DNS / endpoint questions.

> Caveat (accepted): `.local` is reserved for mDNS (RFC 6762). On hosts running
> Avahi/Bonjour, `kubernetes.<cluster>.local` may resolve via multicast instead
> of NetBird's unicast DNS. Operators who hit this set a non-`.local` zone in the
> prompt; the default stays `.local` per decision.

### Usage of the zone

- **Every cluster**: apiserver cert gains the SAN `kubernetes.<dnsZone>`.
- **`type=vpn`** (hosts Mgmt): NetBird Mgmt is configured with
  `--dns-domain={{ .ClusterConfig.NetBird.DNSZone }}`, replacing the hardcoded
  `netbird.selfhosted` at `values-netbird.yaml.tmpl`. One source of truth for the
  zone.

### Cert SAN plumbing

- **`extraCertSANs` made mode-agnostic** — operator-supplied extra SANs work on
  every provider/mode, not just the hcloud LoadBalancer path it lives on today.
- `pkg/core/templates.go` builds the SAN list = `[kubernetes.<dnsZone>]` +
  `extraCertSANs`, emitted for all modes in `values-capi-cluster.yaml.tmpl`.
- kubeaid chart `KubeadmControlPlane.yaml`: **de-gate the `certSANs` block** so it
  renders whenever any SAN exists (today it only renders when `endpoint.host` is
  set), letting the `kubernetes.<dnsZone>` default land on every cluster.

## Repos

- **kubeaid-cli** (this branch): config field + computed default + prompt +
  `templates.go` SAN list + `values-*.yaml.tmpl` (capi-cluster emission, netbird
  `--dns-domain` DRY).
- **kubeaid** (chart, companion PR): `KubeadmControlPlane.yaml` `certSANs` de-gate.

## Out of scope

- Retrofitting existing clusters' live certs (manual `kubeadm init phase certs
  apiserver` regen; documented separately in the SAN troubleshooting).
- Aligning klist's client-side `clusterPeerSuffix` with per-cluster `.local`
  zones (client-side; follow-up).

## Revision (post-merge)

The `<cluster-name>.local` **computed default was dropped**. The DNS zone is
now **operator-supplied only**, collected via the prompt as a **required**
field with no pre-filled value (each NetBird mesh has its own zone; kubeaid-cli
shouldn't invent one). Consequences:

- `parser/netbird.go` no longer defaults `dnsZone` (the `DefaultNetBirdDNSZone`
  helper is removed); `hydrateNetBirdDefaults` is back to stun/turn only.
- `templates.go` adds `kubernetes.<dnsZone>` **only when `cluster.netbird.dnsZone`
  is set** — a cluster with no netbird block / no zone gets no `kubernetes.<zone>`
  SAN (just its operator `controlPlane.extraCertSANs`, if any).
- The prompt asks the zone (required, example `mesh.acme.com`) for both cluster
  types.
