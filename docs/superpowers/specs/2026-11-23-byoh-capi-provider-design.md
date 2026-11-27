# Platform9 BYOH as a CAPI provider in KubeAid CLI — Design

**Date:** 2026-11-23  
**Status:** Proposed  
**Repos affected:** `Obmondo/kubeaid-cli`, `Obmondo/KubeAid` (`argocd-helm-charts/capi-cluster` + BYOH workload templates)  
**Upstream:** [platform9/cluster-api-provider-bringyourownhost](https://github.com/platform9/cluster-api-provider-bringyourownhost)  
**Proposed provider pin:** `v2026.12.0` (same commit as `pcd-v2026.12.0`) — latest Platform9 release tag as of this design; far ahead of classic `v0.5.0` (2023). Final pin is confirmed in Phase 0 against the management-cluster CAPI version we actually deploy.

---

## Goal

Add Platform9’s **Cluster API Provider Bring Your Own Host (BYOH)** to kubeaid-cli as a **CAPI-native provider** for already-provisioned Linux hosts, **side-by-side with KubeOne** (`cloud.bare-metal`). KubeOne is not replaced or migrated in this design.

Target operator flow after Phase 1:

```text
kubeaid-cli config generate          # provider = byoh (experimental)
# install byoh-hostagent on Ubuntu hosts using CLI-generated script
kubeaid-cli cluster bootstrap        # K3D → BYOH CAPI → Cilium → ArgoCD → pivot*
```

\*Pivot behavior is spike-gated (Decision 4).

---

## Problem

kubeaid-cli has two provision models:

| Path | Providers | Mechanism |
|------|-----------|-----------|
| **CAPI** | `aws`, `azure`, `hetzner` | K3D → InfrastructureProvider → Cluster/Machine → ArgoCD → `clusterctl move` |
| **KubeOne** | `bare-metal` | No management cluster; embedded KubeOne over SSH |
| **Local** | `local` | K3D is the cluster |

Generic “customer already has Linux boxes” uses KubeOne because classic CAPI providers create VMs. BYOH fills that niche under CAPI: hosts are registered by an agent into a capacity pool (`ByoHost`), then claimed by `ByoMachine`.

We want CAPI uniformity for bring-your-own hosts without regressing the production KubeOne path.

---

## Background

### BYOH (Platform9 fork)

- Cluster API infrastructure provider for pre-provisioned Linux hosts.
- Hard fork of the old VMware Tanzu project; Platform9 maintains it.
- **Alpha** upstream (no BC guarantee).
- Host agent registers `ByoHost`; CAPI claims hosts for Machines.
- CNI is **not** installed by BYOH.
- Control-plane endpoint is an operator-supplied static IP (kube-vip in upstream templates).

### Compatibility (as analyzed)

| Dimension | BYOH (Platform9) | kubeaid-cli / KubeAid today |
|-----------|------------------|-----------------------------|
| CAPI go.mod | v1.12.11 | kubeaid-cli v1.13.4 |
| CAPI chart default | n/a | `capi-cluster` `global.clusterAPI.version` **v1.11.11** |
| Contract | v1beta1 | v1beta1 |
| OS / arch | Ubuntu **22.04 / 24.04**, **amd64** | KubeOne: broader via SSH |
| Kubernetes (BYOH README) | **v1.31–v1.35** | KubeOne constants: v1.34–v1.36 |
| Stock `clusterctl init --infrastructure byoh` | Still points at **VMware** | Must pin **Platform9** artifacts only |
| `metadata.yaml` | Only series 0.1–0.5 | Calendar tags need pin via Helm/URL we control |

### Why not put BYOH under `cloud.bare-metal`?

`UsingClusterAPI()` returns false for both `bare-metal` and `local`, but that alone does **not** mean “no K3D”: `local` still creates a K3D cluster (it *is* the workload). Only `bare-metal` early-returns in `CreateDevEnv` and skips the management cluster entirely. Putting BYOH under `bare-metal` would still force a dual-mode fork across bootstrap/upgrade/sync (KubeOne vs CAPI). BYOH is a new infrastructure provider, so it gets a new top-level provider key — same as `aws` / `azure` / `hetzner` — and rides the default CAPI path (`UsingClusterAPI()==true`, K3D management cluster included).

---

## Decisions

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Provider shape | New top-level `cloud.byoh` (`CloudProviderBYOH = "byoh"`). **Not** a mode under `bare-metal`. | Matches `getInfrastructureProviderName() == CloudProviderName`; `UsingClusterAPI()` stays true by default; KubeOne untouched. |
| 2 | Coexistence with KubeOne | **Side-by-side.** No removal, no auto-migration in this design. | KubeOne is production (bootstrap + upgrade + sync). BYOH is Alpha. |
| 3 | Provider install | KubeAid `capi-cluster` gains `provider.byoh` + `templates/provider-byoh.yaml`, installed like CAPA/CAPZ/CAPH via ArgoCD `InfrastructureProvider`. Pin Platform9 release/images — **never** stock clusterctl VMware URL. | One ops model with existing clouds. |
| 4 | Pivot (`clusterctl move`) | **Spike-gated.** Prefer pivot + agent re-home. Fallback for v1: **skip pivot** for BYOH (management cluster remains; `--skip-clusterctl-move` or auto-skip). | Agents persist kubeconfig at `~/.byoh/config` aimed at the management API. Upstream does not document move. Controllers honor CAPI pause, but agent connectivity after move is unproven. |
| 5 | Host registration UX | CLI creates per-host `BootstrapKubeconfig` + generates an install script. **No** Platform9 `byohctl onboard` (SaaS-coupled). Full SSH remote install is Phase 2. | Vendor-neutral; enough for bootstrap without owning SSH packaging on day one. |
| 6 | Lifecycle scope (v1) | **bootstrap + delete.** Upgrade = GitOps bump (refuse `cluster upgrade` with a clear message). Sync = ArgoCD only (existing refuse path). Recover = out of scope. | Same scope pattern as EKS design for CAPI managed paths. |
| 7 | OS / arch / k8s gates | Parse/prompt: Ubuntu 22.04 or 24.04, amd64; k8s in BYOH range for the pinned release (initially v1.31–v1.35, re-verify). | Upstream matrix is hard; KubeOne covers everything else. |
| 8 | CNI | **Cilium only**, installed by kubeaid-cli after CP ready. | Stack uniformity; BYOH does not install CNI. |
| 9 | Control-plane endpoint | Required static `controlPlane.endpoint.host` (+ port); chart renders kube-vip. | Same idea as KubeOne `BareMetalControlPlane.Endpoint` / BYOH `CONTROL_PLANE_ENDPOINT_IP`. |
| 10 | Installer / bundles | Default Platform9 bundle registry (`bundleLookupBaseRegistry` / `K8sInstallerConfig`). `skipInstallation` config for golden images later (Phase 2). | Correct default for Ubuntu matrix. |
| 11 | Product posture | Experimental / documented Alpha. Not the default for existing bare-metal customers. | Honest support surface. |
| 12 | CAPI version pins | Phase 0 spike validates BYOH against the **deployed** management-cluster CAPI version (chart). Adjust chart and/or BYOH pin as a set — do not silently mix 1.11 / 1.12 / 1.13. | Chart already lags go.mod; BYOH makes skew first-class. |
| 13 | Provider name in YAML | `byoh` | Matches infrastructure provider name / `infrastructure-byoh` label. |

---

## BYOH contracts kubeaid-cli must honor

### API

- **apiVersion:** `infrastructure.cluster.x-k8s.io/v1beta1`
- **Kinds we care about:** `ByoCluster`, `ByoHost`, `ByoMachine`, `ByoMachineTemplate`, `BootstrapKubeconfig`, `K8sInstallerConfig` / `K8sInstallerConfigTemplate` (plus optional `ByoHostAgentUpgrade` later)

### Minimal workload objects (1 CP + N workers)

From upstream e2e templates (`test/e2e/data/infrastructure-provider-byoh/...`):

| Object | Purpose |
|--------|---------|
| `Cluster` | CAPI root |
| `ByoCluster` | Infra + `controlPlaneEndpoint` + bundle registry |
| `KubeadmControlPlane` | CP bootstrap (kube-vip, BYOH template vars) |
| `ByoMachineTemplate` (CP) | CP infra + `installerRef` |
| `K8sInstallerConfigTemplate` (CP) | k8s component bundles |
| `MachineDeployment` | Workers |
| `ByoMachineTemplate` + `KubeadmConfigTemplate` (workers) | Worker infra + join |
| `K8sInstallerConfigTemplate` (workers) | Worker bundles |

`ByoHost` is **not** in the cluster template — the agent creates it at registration.  
`BootstrapKubeconfig` is created by the CLI (or operator) **before** Machines can claim hosts.

### Hard requirements (wrong = hard fail)

1. **Namespace match** — agent `--namespace` == `BootstrapKubeconfig` namespace == `ByoHost` / `ByoMachine` namespace. That is the **workload CAPI namespace** (kubeaid-cli uses `capi-cluster`), **not** the BYOH controller install namespace. Upstream agent default is `default`; we default to `capi-cluster` to match existing CAPA/CAPZ/CAPH object placement.  
2. **`nodeRegistration.name: "{{ .ByoHostName }}"`** on every kubeadm init/join template.  
3. **`CONTROL_PLANE_ENDPOINT_IP`** — free IP on the CP subnet, outside DHCP; wired into `ByoCluster` + kube-vip.  
4. Agent-rendered `{{ .DefaultNetworkInterfaceName }}` for kube-vip interface.

### BootstrapKubeconfig

- Spec requires `apiserver`, `certificate-authority-data`, and **`hostName`** (immutable, normalized).  
- Status historically exposes `bootstrapKubeconfigData` (still what e2e reads); `credentialSecretRef` is the documented future contract — implement against what the pinned release actually populates (verify in Phase 0).  
- One CR **per host**.

### Agent install (CLI-generated script, v1)

Prefer Platform9 agent package path over the stale getting-started binary-only flow:

1. Host packages: `ebtables`, `conntrack`, `socat`, `ethtool`, (imgpkg for OCI pull).  
2. Pull/install agent from Platform9 OCI (`quay.io/platform9/cluster-api-provider-bringyourownhost/agent:<tag>`) or release asset.  
3. Write bootstrap kubeconfig; start agent with `--bootstrap-kubeconfig` and `--namespace`.  
4. Do **not** pre-create `~/.byoh/config` — agent creates it after CSR.  
5. Root required.

### Pivot implication

If `~/.byoh/config` already exists, the agent **skips** bootstrap and keeps talking to that API server. After `clusterctl move`, agents must be **re-homed** (clear config + new BootstrapKubeconfig) or the management endpoint must remain reachable. This is the Phase 0 critical test.

### Recommended pin

- Tag: **`v2026.12.0`** (same commit as `pcd-v2026.12.0`)  
  - Why this tag: newest Platform9 release on the fork at design time; includes post-`v0.5.0` agent/controller work and quay.io/Platform9 packaging. Classic `v0.5.0` is years stale and not what we should integrate against.  
  - Why still “proposed”: Phase 0 must prove this tag against our deployed management-cluster CAPI version (chart `global.clusterAPI.version` may need a coordinated bump).  
- Controller image: `quay.io/platform9/cluster-api-provider-bringyourownhost/controller-manager:<tag>`  
- Bundle registry default: `quay.io/platform9`  
- Management cluster needs **cert-manager** (BYOH webhooks) — already true for our CAPI path.

---

## Configuration surface

### `general.yaml` (proposed)

```yaml
cloud:
  byoh:
    controlPlane:
      endpoint:
        host: 10.10.10.10   # required static VIP / free IP
        port: 6443
      replicas: 3
      # Optional host claim filter. Friendly key in general.yaml;
      # chart must emit ByoMachineSpec.selector (*metav1.LabelSelector).
      # selector:
      #   matchLabels:
      #     role: control-plane

    nodeGroups:
      - name: workers
        replicas: 3
        # selector:
        #   matchLabels:
        #     role: worker

    bundleLookupBaseRegistry: quay.io/platform9

    os:
      version: "24.04"          # 22.04 | 24.04
      arch: amd64

    agent:
      # Must match BootstrapKubeconfig / ByoHost / ByoMachine namespace.
      # Default = kubeaid-cli CAPI workload ns (capi-cluster), NOT the
      # controller install namespace. Upstream agent flag default is "default".
      namespace: capi-cluster
      # skipInstallation: false  # Phase 2

    # Checklist for BootstrapKubeconfig generation (not SSH inventory).
    hosts:
      - hostName: cp-1
        role: control-plane
      - hostName: worker-1
        role: worker
```

**Field mapping note:** if `general.yaml` keeps a friendlier alias later, document it explicitly — the BYOH API field on `ByoMachine` / `ByoMachineTemplate` is `spec.selector` (`*metav1.LabelSelector`), not `hostSelector`. Chart authors must emit `selector`.

No cloud credentials / `secrets.yaml` block for BYOH.

### Prompt (sketch)

```text
provider = byoh
  → experimental / Alpha warning + Ubuntu 22.04|24.04 amd64 only
  → control-plane endpoint host/port
  → CP replicas + worker node groups
  → hostnames (for BootstrapKubeconfig)
  → bundle registry (default Platform9)
  → agent namespace
```

### Validation

- Exactly one of `cloud.aws|azure|hetzner|bare-metal|byoh|local`.  
- Endpoint host required; CP replicas ≥ 1.  
- OS/arch gates when set; k8s version in BYOH-supported range.  
- VPN cluster type remains Hetzner-only (existing rule).

---

## Provisioning flow

BYOH uses the **CAPI path**, not KubeOne:

```text
CreateDevEnv
  ├─ clone kubeaid-config
  ├─ create K3D management cluster          # NOT BareMetal early-return
  └─ SetupCluster(management): ArgoCD + cluster-api-operator + InfrastructureProvider byoh

Host capacity (CLI assist)
  ├─ create BootstrapKubeconfig per hosts[].hostName
  ├─ print / write agent install script
  └─ wait until enough ByoHosts are registered

Apply Cluster / Byo* / KCP / MD via ArgoCD capi-cluster (provider.byoh: true)
  └─ templates include ByoHostName + kube-vip + installerRefs

Wait Cluster/Machines Ready → install Cilium → SetupCluster(main) / GitOps

Pivot (Decision 4)
  ├─ if spike OK: clusterctl move + agent re-home + verify heartbeats
  └─ else: skip pivot for BYOH; document long-lived management cluster
```

### Non-goals (this design)

- Replacing or auto-migrating KubeOne clusters.  
- Using BYOH for Hetzner Robot (CAPH owns that).  
- Non-Ubuntu / arm64 GA.  
- Platform9 SaaS `byohctl onboard` as the supported path.  
- Disaster recovery parity.  
- Agent fleet auto-upgrade GA.

---

## Concrete code changes

### kubeaid-cli

| Area | Files | Change |
|------|-------|--------|
| Constant | `pkg/constants/constants.go` | `CloudProviderBYOH = "byoh"`; `Min/MaxBYOHSupportedK8sVersion` |
| Config | `pkg/config/general.go` | `CloudConfig.BYOH *BYOHConfig` (+ nested types above) |
| Parser | `pkg/config/parser/parse.go` | `detectCloudProviderName`, `setCloudProvider` → `byoh.NewBYOHCloudProvider()`, `hydrateVMSpecs` empty case |
| Validate | `pkg/config/parser/validate.go` | `validateBYOHConfig`, k8s range gate |
| Prompt | `pkg/config/prompt/prompt.go`, `provider.go`, **new** `provider_byoh.go`, `state_helpers.go`, `resume.go` | Register option + collect fields |
| Render | `pkg/render/config.go`, `pkg/render/templates/general.yaml.tmpl` | BYOH branch |
| Templates | `pkg/core/templates.go`, `pkg/constants/templates.go` | `TemplateValues.BYOHConfig`; **CommonCloudSpecific** path (Cilium + CAPI apps) — **never** BareMetal/KubeOne replacement list |
| values | `pkg/core/templates/argocd-apps/values-capi-cluster.yaml.tmpl` | `{{- if .BYOHConfig }}` → `provider.byoh: true` + `byoh:` block |
| Cloud provider | **new** `pkg/cloud/byoh/` | Interface stubs; BootstrapKubeconfig helpers; install-script writer |
| Bootstrap | `pkg/core/bootstrap_cluster.go` | Host-wait phase when BYOH; pivot skip/re-home per Decision 4; guard snapshot-controller sync if templates absent |
| CreateDevEnv | `pkg/core/create_dev_env.go` | No BareMetal early-return for BYOH (automatic if constant ≠ bare-metal) |
| Infra wait | `pkg/core/setup_cluster.go` | `getInfrastructureProviderName()` → `"byoh"` (automatic) |
| Scheme | `pkg/utils/kubernetes/kubernetes.go` | Register BYOH v1beta1 scheme when CLI creates/lists BYOH CRs |
| Helpers | `IsNodeGroupCountZero` etc. | Add BYOH cases where switches list providers explicitly |
| Upgrade cmd | `cmd/.../upgrade/upgrade.go` | Explicit refuse (GitOps message) — **required** or `byoh` falls into wrong “local” default assert |
| Sync cmd | `cmd/.../sync/sync.go` | Already refuses non-bare-metal — OK |
| Recover | `cmd/.../recover` / `recover_cluster.go` | Explicit refuse / unimplemented |
| Delete | `pkg/core/delete_cluster.go` | Use CAPI path; print agent decommission steps |
| Docs | `README.md`, `docs/architecture.md` | Third story: Hetzner BM (CAPH) vs generic BM (KubeOne) vs BYOH |
| Tests | parser/prompt/template goldens | Detect `byoh`, render `values-capi-cluster`, `UsingClusterAPI==true` |

**KubeOne path:** no intentional edits beyond not colliding on shared switches.

### KubeAid (`argocd-helm-charts/capi-cluster`)

| Path | Change |
|------|--------|
| `values.yaml` | `provider.byoh: false`; `global.byoh.version: v2026.12.0` (or agreed pin) |
| `Chart.yaml` | Optional subchart dependency `condition: provider.byoh` |
| **new** `templates/provider-byoh.yaml` | `InfrastructureProvider` name `byoh`, Platform9 `fetchConfig.url` or equivalent image pin |
| **new** `charts/byoh/**` | Cluster, ByoCluster, ByoMachineTemplate(s), K8sInstallerConfigTemplate(s), KCP, KubeadmConfigTemplate, MachineDeployment, kube-vip, `{{ .ByoHostName }}` |

`provider-byoh.yaml` must mirror CAPA/CAPZ/CAPH naming so `syncInfrastructureProvider` can wait on label `cluster.x-k8s.io/provider=infrastructure-byoh`.

---

## Day-2 matrix (v1)

| Operation | CAPI clouds | KubeOne `bare-metal` | BYOH |
|-----------|-------------|----------------------|------|
| bootstrap | CAPI + pivot | KubeOne apply | CAPI + agents (+ pivot TBD) |
| upgrade | GitOps / helpers | `cluster upgrade` | Refuse CLI; GitOps bump |
| sync | ArgoCD | `cluster sync` | ArgoCD (CLI refuses) |
| delete | CAPI | incomplete today | CAPI + document agent stop |
| recover | AWS/Azure | unimplemented | out of scope |

---

## Risks and mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Post-pivot agent orphaning | Critical | Phase 0 spike; fallback = skip pivot |
| Upstream Alpha | High | Pin exact release; experimental UX; keep KubeOne |
| CAPI version skew | High | Spike against chart CAPI; pin set together |
| Stock clusterctl → VMware | High | Only Platform9 URL/images in our chart |
| Narrow OS/arch | Medium | Hard validation; KubeOne for other hosts |
| First-match host claim | Medium | `ByoMachineSpec.selector` matchLabels; avoid shared dirty pools |
| Wrong endpoint IP / kube-vip | Medium | Prompt + docs |
| Two-repo delivery | Medium | Chart + CLI same release train |
| “Three bare metals” confusion | Medium | Naming in architecture docs |
| BootstrapKubeconfig status API drift | Medium | Verify pinned release in Phase 0 (`bootstrapKubeconfigData` vs Secret) |

---

## Phased plan

### Phase 0 — Spike (before provider code)

1. Management cluster (Kind/K3D) + cert-manager.  
2. Install **Platform9** BYOH (`v2026.12.0` or spike-chosen pin).  
3. Register 1 CP + 1 worker; form cluster; install Cilium.  
4. Attempt `clusterctl move`; test agent heartbeats / new claims; document re-home steps (`~/.byoh/config`).  
5. Record CAPI version, images, pivot result.  
6. Append spike results to this doc; keep or flip Decision 4 fallback.

**Exit:** proceed with pivot, proceed without pivot, or stop.

### Phase 1 — Thin integration (if Phase 0 green)

- `cloud.byoh` config/prompt/validate.  
- KubeAid `provider.byoh` + templates.  
- Bootstrap + Cilium + ArgoCD + delete.  
- Pivot behavior per spike.  
- Docs + experimental warning.  
- KubeOne unchanged.

### Phase 2 — Hardening

- Optional SSH agent installer.  
- GitOps upgrade runbook / thin helper.  
- Host label pools UX.  
- `skipInstallation` for golden images.  
- Evaluate agent upgrade CR.  
- Optional KubeOne → BYOH migration guide (still optional).

---

## Testing

| Layer | What |
|-------|------|
| Unit | Detect/validate `byoh`; template render; `UsingClusterAPI()==true` |
| Spike | Phase 0 manual e2e documented |
| CI | Manifest/chart render smoke; full host e2e out of default CI initially |
| Staging | One internal Ubuntu 22.04/24.04 cluster before offering to customers |

---

## Success criteria

1. Phase 0 answers pivot + version pins with evidence.  
2. Phase 1 bootstraps 1 CP + ≥1 worker via kubeaid-cli on Ubuntu 24.04 amd64.  
3. KubeOne `bare-metal` has zero intentional regressions.  
4. Docs distinguish Hetzner BM (CAPH) / generic BM (KubeOne) / BYOH.  
5. This doc stays the source of truth; Status moves to **Approved** when the proposal is accepted, then **Implemented** with an addendum for deltas.

---

## Appendix A — Related docs

- `docs/superpowers/specs/2026-08-09-eks-support-design.md` — variant vs new provider, lifecycle scope, two-repo charts  
- `docs/superpowers/specs/2026-07-05-kubeone-day2-upgrade-design.md` — why KubeOne remains a sibling path  
- `docs/architecture.md` — update provider table after implementation

## Appendix B — One-paragraph summary

Add an experimental top-level `cloud.byoh` CAPI provider that installs Platform9 BYOH through the KubeAid `capi-cluster` chart, assists host registration via BootstrapKubeconfig + install script, and keeps KubeOne `bare-metal` unchanged. Spike pivot/agent re-home before coding; if pivot fails, ship v1 without discarding the management cluster. Gate Ubuntu 22.04/24.04 amd64 and pin Platform9 `v2026.12.0` (or the spike-proven tag) against a known management-cluster CAPI version.
