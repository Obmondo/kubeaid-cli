# NetBird optional for workload clusters (prompt gate)

Date: 2026-07-07
Status: Approved → implementing

## Problem

Since the 2026-06-13 NetBird DNS-zone change
(`docs/specs/2026-06-13-netbird-dns-zone-cert-sans.md`), `config generate`
forces the mesh DNS zone (`cluster.netbird.dnsZone`) on **every** cluster — the
prompt is required for both `vpn` and `workload` types. But a workload cluster
does not have to join a NetBird mesh. Today:

- The zone is collected unconditionally and required, up front
  (`collectNetBirdDNSZoneIfNeeded`, `pkg/config/prompt/prompt.go:502`), and
  rendered to `cluster.netbird.dnsZone` for every workload cluster.
- The NetBird **Management endpoint** (`cluster.netbird.dns`) and the operator
  **service-user API key** are collected only *inside* the lockdown step
  (`runWorkloadLockdownForm`, `prompt.go:608`), and only for **Hetzner
  bare-metal / hybrid** workload clusters (`collectWorkloadLockdownIfNeeded`,
  `prompt.go:541`, bails via `hetznerUsesBareMetal` for every other mode).

Two consequences:

1. A workload cluster that is **not** on a NetBird mesh is still forced to
   invent a mesh DNS zone, and its rendered general.yaml carries a half
   `cluster.netbird` block (zone only, no `dns`) that installs no operator.
2. A workload cluster on AWS / Azure / hcloud-only that **does** want to join a
   mesh has **no prompt path** to set `cluster.netbird.dns` — the operator can
   only be turned on via the Hetzner-BM lockdown branch.

We want NetBird to be an explicit, provider-independent choice for workload
clusters: ask whether the cluster joins a mesh; if yes, collect the Management
URL + mesh domain + operator API key and install the operator; if no, emit no
`cluster.netbird` block and no operator.

## Scope

- **Workload clusters only.** VPN clusters host NetBird by definition and are
  unchanged (zone still required, endpoints / Keycloak flow untouched).
- Downstream rendering already tolerates a workload cluster with no NetBird:
  `OperatorEnabled()` (`pkg/core/netbird/derive.go:19`) is false when
  `cluster.netbird.dns` is empty, the `kubernetes.<dnsZone>` apiserver cert SAN
  is added only when `dnsZone` is set (per the 2026-06-13 revision), and the
  operator ArgoCD app / SealedSecret are appended only when `OperatorEnabled()`.
  So this is a **prompt-layer change plus one gated block in general.yaml** — no
  changes to `templates.go`, `derive.go`, or the netbird-operator templates.

## Design

### The workload NetBird gate

Replace the unconditional zone step, **for workload clusters**, with a gate:

- **Confirm:** "Is this cluster joining a NetBird mesh?" (yes / no).
- **No** → record the decision; collect nothing. No `cluster.netbird` block, no
  operator, no `kubernetes.<zone>` cert SAN, and lockdown is not offered (no
  mesh ⇒ nothing to fall back to after locking down the public NIC — confirmed
  by the operator: "if netbird is set to no, then lockdown won't matter much,
  since the cluster is bootstrapped without internal one").
- **Yes** → collect, **all required**:
  - **NetBird Management URL** → `cluster.netbird.dns`
    (existing `PromptedConfig.NetBirdDNS`; `derive.go` turns it into
    `https://<dns>`).
  - **Mesh domain (internal apps DNS zone)** → `cluster.netbird.dnsZone`
    (existing `PromptedConfig.NetBirdDNSZone`).
  - **NetBird service-user API key** → `secrets.yaml` `netbird.apiKey`
    (existing `PromptedConfig.NetBirdAPIKey`).

  Operator installs (because `cluster.netbird.dns` is now set); lockdown is
  offered next for Hetzner BM/hybrid, as today.

VPN clusters keep the existing `runNetBirdDNSZoneForm` (zone required).

### Reuse the existing dashboard token-creation note

The step-by-step "create the service-user token in the NetBird dashboard"
instructions already exist in `runWorkloadLockdownForm`
(`pkg/config/prompt/prompt.go:632-647` — the `steps` string shown via
`huh.NewNote().Title("NetBird operator API key")`). The new join form **reuses
that same note verbatim** (moved, not rewritten) so there is one source of truth
for the token instructions:

```
Create the token in the NetBird dashboard:
  https://<netbird-mgmt-dns>/  →  Team  →  Service Users  →  + Create Service User
    Name:  k8s-operator        Role:  Admin
  From the new user's row  →  ⋮  →  Tokens  →  + Generate Token
    Name:  kubeaid-<cluster>   Expiration:  the longest offered
    (the token is shown only once — copy it)
```

### Lockdown becomes NetBird-gated and note-only

`runWorkloadLockdownForm` loses its NetBird sub-form (Mgmt DNS + API key + token
note) — those move into the join form above. It becomes the CCNP note + the
single "Lock down this cluster?" confirm. `collectWorkloadLockdownIfNeeded`
gains a precondition: only offer lockdown when the cluster is joining a mesh (in
addition to the existing workload + Hetzner-BM/hybrid guard).

### Flow / ordering

Unchanged positions; the netbird step just branches by cluster type:

1. basics
2. cluster auth (vpn: Keycloak; workload: none)
3. **netbird step** — vpn: zone form (as today); **workload: new join gate**
   (yes ⇒ Mgmt URL + zone + API key). Stays before provider credentials; the
   fields are provider-independent.
4. provider credentials
5. **lockdown** (workload + joining-mesh + Hetzner BM/hybrid) — now just the
   CCNP confirm
6. git / SSH

### Enablement flag + rendering

- `PromptedConfig.NetBirdBlockEnabled()` helper, **derived** (no new field):
  `ClusterType == vpn || NetBirdDNSZone != ""`. This mirrors `pkg/core/netbird`'s
  derive-from-config idiom (`OperatorEnabled()` keys off `dns != ""`) — the
  join form sets the zone on "yes", leaves it empty on "no", so the zone's
  presence is the enabled signal. Simpler than a separate `*bool`, and existing
  render tests that set a zone stay green.
- **general.yaml.tmpl**: wrap the whole `netbird:` block in
  `{{- if .NetBirdBlockEnabled }} … {{- end }}`. VPN and workload+yes render it;
  workload+no omits it.
- **secrets.yaml.tmpl**: unchanged — `netbird.apiKey` is already emitted only
  when `.NetBirdAPIKey` is set.

### Resume / state

The existing `promptState.NetBirdDNSZone` bool is reused as the "NetBird step
done" flag for both cluster types — set even when a workload declines, so a
resumed session (state file present) doesn't re-prompt.

Completion is derived by `netBirdStepDone(cfg)` rather than a bare
`NetBirdDNSZone != ""`, and this is the load-bearing detail. VPN: zone set.
Workload: **Mgmt URL (`dns`) and mesh zone must agree** — both set (joined) or
both empty (declined). This matters for a **legacy** workload config produced by
the old unconditional-zone code, which set `dnsZone` but only ever set `dns`
inside the Hetzner-BM lockdown branch — leaving zone-without-dns on every other
provider. Without the agreement check, the resume fallback
(`completedPromptStateFromValues`, used when config files exist but no state
file) would mark such a config "done", skip the gate forever, and render a half
`netbird` block (`NetBirdBlockEnabled` is zone-gated → true) that installs no
operator (`OperatorEnabled` is `dns`-gated → false) — silently, since nothing
validates the combination on workload clusters. `netBirdStepDone` reports the
zone-only legacy state as **not done**, so the gate re-opens and the operator
supplies the Mgmt URL + key (or clears the zone). It's used in both
`completedPromptStateFromValues` and the collect guards (defending against a
stale state file too).

The join-gate confirm defaults via `netBirdJoinDefault(cfg)`
(`dns != "" || dnsZone != ""`) rather than a hardcoded `true`, so the edit loop
(`runPromptLoop` resets state but keeps cfg) re-shows the operator's previous
choice instead of flipping a decline back to "Yes" — matching the cfg-derived
default convention used elsewhere (e.g. `runVPNKeycloakForm`).

Stale doc-comments in that area are corrected: `prompt.go` NetBird field
comments, and the `general.go` `DNSZone` "`<cluster>.local` default" claim
(`hydrateNetBirdDefaults` never defaulted it) — the latter regenerated into
`docs/config-reference.md` via `make run-generators`.

## Testing

- Prompt: table tests for the workload join gate via `runNetBirdJoinForm` /
  `runNetBirdZoneForm` seams (decline ⇒ all three fields empty + step done;
  join ⇒ fields collected + trimmed; vpn ⇒ zone form, not the gate).
- Helper: `NetBirdBlockEnabled()` truth table (vpn, workload+zone,
  workload-no-zone).
- Rendering: general.yaml assertions for workload+no (no `netbird:`/`dnsZone:`),
  plus the existing workload+netbird and vpn render tests stay green.
- Lockdown: only offered when joining a mesh (skipped when `NetBirdDNS == ""`),
  and the slimmed form sets only `Lockdown`; unchanged for VPN.
- Summary: `workloadNetBirdSummaryLines()` cases (declined; joined without a
  lockdown decision; joined + lockdown) so the summary reflects the mesh join
  independent of lockdown.

## Out of scope

- VPN cluster flow (unchanged).
- Any change to `templates.go` / `derive.go` / netbird-operator chart templates
  — the "off" state is already supported there.
- Retrofitting already-generated workload configs (operators re-run
  `config generate` or hand-edit).

## Repos

- **kubeaid-cli** (this branch): `pkg/config/prompt/` (new workload join form +
  gate, lockdown form slimmed, state plumbing), `general.yaml.tmpl` block guard,
  `PromptedConfig` field + helper, tests + fixtures.
