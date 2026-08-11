# AKS Support in KubeAid CLI — Design

**Date:** 2026-08-11
**Status:** Implemented (kubeaid-cli side) — the KubeAid `capi-cluster` chart side is
follow-up work, see "Chart changes" below
**Repos affected:** `Obmondo/kubeaid-cli` (this change), `Obmondo/kubeaid` (the
`capi-cluster` Helm chart — follow-up)

## Goal

Let `kubeaid-cli` provision and manage **Azure AKS** clusters (Azure-managed control
plane) alongside the existing self-managed CAPZ-based Azure clusters, using the same
GitOps flow: throwaway K3D management cluster → Cluster API provisions the target →
`clusterctl move` pivots → ArgoCD reconciles the KubeAid addon stack.

This mirrors the EKS design (`2026-08-09-eks-support-design.md`) wherever the two
clouds behave the same, and deviates only where AKS forces it to.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | AKS is a **variant of the existing Azure provider** (`cloud.azure.aks: true`), not a sixth cloud provider | Same reasoning as EKS decision 1: the infrastructure provider stays CAPZ (`azure`); credentials and the provider-name assumptions are shared. |
| 2 | Workers are **`AzureManagedMachinePool`s** (AKS agent pools), one per node-group; the first node-group renders as the required `System` mode pool, the rest as `User` pools | Unlike CAPA, CAPZ managed clusters support **only** machine pools — MachineDeployments cannot attach to an AKS control plane. `AzureManagedControlPlane` + `AzureManagedCluster` + `AzureManagedMachinePool` is CAPZ's GA managed-cluster path (CAPZ v1.21.1, the version the KubeAid chart pins). The CAPI `MachinePool` feature flag is enabled by default. |
| 3 | Node autoscaling uses **AKS's built-in cluster autoscaler** (`AzureManagedMachinePool.spec.scaling` from the node-group's minSize/maxSize); the cluster-autoscaler addon is dropped | The CAPI-mode cluster-autoscaler manages MachineDeployments; AKS agent pools are scaled by AKS itself. Managed autoscaler needs no in-cluster pod and no RBAC. |
| 4 | No machine images and no SSH keys to pick — `sshPublicKey`, `canonicalUbuntuImage`, and `controlPlane.*` must stay **unset** when `aks: true` | AKS owns node images and control-plane sizing. Nodes stay keyless (use `az aks command invoke` / node-shell for debugging), matching the EKS keyless-workers decision. |
| 5 | **Cilium is the CNI, kube-proxy-free**: the chart sets `AzureManagedControlPlane.spec.networkPlugin: none` (BYO CNI — an allowed enum value in CAPZ) **and disables kube-proxy** via `ASOManagedClusterPatches` (`networkProfile.kubeProxyConfig.enabled: false` on the underlying ASO ManagedCluster — the classic CAPZ API has no first-class field for it, up to and including v1.26.0). kubeaid-cli then helm-installs the KubeAid cilium chart with `kubeProxyReplacement=true` once the control plane is ready — identical to the EKS flow | Stack uniformity (network policy, Hubble, kube-proxy replacement) across KubeAid clusters. Setting `kubeProxyReplacement` explicitly also sidesteps Cilium's kube-proxy auto-detection, which AKS's `windows-kube-proxy-initializer` DaemonSet is known to confuse. BYO-CNI worker nodes stay NotReady until the CNI lands, so the install is a bootstrap step, not an ArgoCD sync; the cilium ArgoCD App adopts the manifests afterwards. Caveat: AKS's kube-proxy configuration may still require the `KubeProxyConfigurationPreview` feature registered on the subscription (pair the patch with CAPZ's `EnablePreviewFeatures` if so) — verify during the chart work. |
| 6 | The prompt asks **self-managed vs AKS before credentials** | Same shape as AWS: credentials are identical either way, but AKS skips the HA question (Azure runs the control plane) and the storage-account derivation (decision 8). |
| 7 | Lifecycle scope: **bootstrap + delete**. `cluster upgrade` and `cluster recover` are rejected for AKS with pointers to the GitOps flow / roadmap | Same as EKS decision 7: an upgrade is a GitOps edit (bump `global.kubernetes.version` in `values-capi-cluster.yaml`; ArgoCD syncs; CAPZ upgrades the AKS control plane and rolls the agent pools). |
| 8 | Credentials: AKS uses the **AAD service principal directly**, via an `AzureClusterIdentity` of type `ServicePrincipal` whose client secret is sealed into the existing `capi-cluster/cloud-credentials` Secret. The **entire workload-identity machinery is skipped**: no Crossplane, no storage-account OIDC provider, no UAMIs, no K3D service-account-issuer key mounts, no azure-workload-identity webhook | The self-managed Azure flow needs all of that because CAPZ authenticates via Workload Identity against an operator-hosted OIDC issuer. AKS clusters can lean on AKS's own OIDC issuer (`oidcIssuerProfile.enabled`) for workload IAM later — deferred exactly like IRSA was for EKS. Dropping the machinery removes the `storageAccount`, `workloadIdentity`, and `aadApplication` config requirements for AKS. |
| 9 | **No storage addons**: AKS ships the Azure Disk / Azure File CSI drivers, their StorageClasses, and the snapshot controller as managed components — `azuredisk-csi-driver`, `external-snapshotter`, and `ccm-azure` are all dropped from the AKS addon set | AKS runs the cloud controller manager and the CSI stack itself. The AKS addon template set is therefore empty beyond the common set (ArgoCD, cert-manager, sealed-secrets, cilium, cluster-api). |
| 10 | **Disaster recovery is rejected** for AKS in validation (`cloud.disasterRecovery` must be unset) | Azure DR provisioning rides the Crossplane + storage-account + Velero-UAMI machinery that decision 8 removes. DR for AKS needs its own design (likely AKS OIDC + workload identity for Velero); deliberately deferred, not forgotten. |

## Configuration surface

### `general.yaml`

```yaml
cloud:
  azure:
    tenantID: <tenant>
    subscriptionID: <subscription>
    location: westeurope
    aks: true            # NEW — selects the Azure managed (AKS) control plane
    nodeGroups:          # unchanged shape; becomes AzureManagedMachinePools
      - name: pool0      # AKS pool names: 1-12 chars, lowercase alphanumeric,
        vmSize: Standard_D2s_v3   # must start with a letter (validated)
        diskSizeGB: 128
        minSize: 3
        maxSize: 9
```

Validation when `aks: true`:

- `controlPlane.*`, `sshPublicKey`, `canonicalUbuntuImage`, `storageAccount`,
  `workloadIdentity`, and `aadApplication` are **rejected** with clear errors —
  they have no meaning on AKS (decisions 4 and 8).
- `cloud.disasterRecovery` is rejected (decision 10).
- At least one node-group is required (an AKS cluster needs a System agent pool
  and has no control-plane nodes to schedule on), and each node-group name must
  be a valid AKS agent-pool name (regex `^[a-z][a-z0-9]{0,11}$`).

The previously struct-tag-required fields (`storageAccount`, `sshPublicKey`,
`canonicalUbuntuImage`, `controlPlane`, `workloadIdentity`, `aadApplication`)
move to cross-field validation in `validateAzureSelfManagedConfig`, mirroring
what the EKS change did to `AWSConfig`.

`secrets.yaml` is unchanged: the same `azure.clientID` / `azure.clientSecret`
service-principal pair. For AKS it is additionally sealed into the
`capi-cluster/cloud-credentials` Secret that the chart's ServicePrincipal
`AzureClusterIdentity` references.

### Prompt flow (`pkg/config/prompt/provider_azure.go`)

```
provider = Azure
  → NEW select: "Control plane: self-managed (VMs, kubeadm) / AKS (managed by Azure)"
  → credentials form (tenant / subscription / client ID / client secret)   (unchanged)
  → [self-managed] HA confirm + storage-account derivation                 (unchanged)
  → [AKS]          nothing — straight on
```

The select writes `PromptedConfig.AzureAKS`, consumed by
`pkg/render/templates/general.yaml.tmpl`, which gains an AKS branch (no
`storageAccount`, no `controlPlane` block).

## Provisioning flow changes (kubeaid-cli)

1. **Management cluster (K3D)** — the service-account-issuer key mounts and
   issuer URL are skipped for AKS (`k3d.go` gates on `!config.AKSEnabled()`).
2. **Cluster-api-operator values** — unchanged: the kubeadm providers stay (the
   delete path needs them in the management cluster), CAPZ ships the managed
   control-plane support in the main provider. The `configSecret` block stays
   azure-suppressed; CAPZ credentials flow through `AzureClusterIdentity`.
3. **Setup cluster** — the whole Crossplane block (install, provider setup,
   `ProvisionInfrastructure`, `CreateOIDCProvider`, `GetInfrastructureDetails`,
   config re-render) is skipped for AKS.
4. **Addon set** — AKS renders no azure-specific addon templates (decision 9);
   secret templates swap the crossplane credentials + service-account-issuer
   keys for the `capi-cluster/cloud-credentials` Secret (client secret).
5. **Provisioned wait** — `WaitForMainClusterToBeProvisioned`'s predicate
   becomes managed-control-plane aware: managed clusters have **no
   control-plane Machines** (so the `cpRunning > 0` gate can never fire), and
   BYO-CNI worker nodes can't reach the v1beta2 `Ready` aggregate before the
   CNI is installed — which only happens *after* this wait. For managed CPs the
   predicate is now: Cluster `Provisioned`+`Ready`, plus (when worker Machines
   exist — EKS) at least one worker Machine `Running` with a Node ref. AKS
   agent pools produce no Machine objects at all, so the Cluster conditions
   carry the gate alone. *This also fixes a latent EKS deadlock — the EKS path
   had not yet been exercised against real AWS.*
6. **CNI install** — `installCiliumOnManagedCluster` (generalised from
   `installCiliumOnEKSCluster`): both managed flavours install Cilium with
   kube-proxy replacement on — kube-proxy is disabled declaratively on both
   (EKS: `spec.kubeProxy.disable`; AKS: the chart's ASO patch, decision 5).
7. **Post-pivot syncs** — the cluster-autoscaler and external-snapshotter
   ArgoCD app syncs are skipped for AKS (the apps aren't rendered; decision 3
   and 9). The EKS credential-zeroing skip is AWS-only and doesn't apply.
8. **Guards** — `cluster upgrade` and `cluster recover` error out for AKS with
   the GitOps-flow / roadmap pointers, mirroring EKS.

## Chart changes (KubeAid repo, `argocd-helm-charts/capi-cluster`) — FOLLOW-UP

Behind a new `azure.aks` values flag in `charts/azure/`:

- **Bump `global.capz.version`** from v1.21.1 to **v1.26.0** (latest upstream,
  June 2026) — same move the EKS work made for CAPA. The classic managed API,
  `networkPlugin: none`, and `ASOManagedClusterPatches` are all verified
  present at v1.26.0.
- **`AzureManagedControlPlane`** (version, location, resourceGroup,
  `networkPlugin: none`, `oidcIssuerProfile.enabled: true`, `identityRef`,
  and `asoManagedClusterPatches` disabling kube-proxy —
  `{"spec": {"networkProfile": {"kubeProxyConfig": {"enabled": false}}}}`,
  plus `enablePreviewFeatures: true` if `KubeProxyConfigurationPreview` is
  still subscription-gated) + **`AzureManagedCluster`**; the `Cluster`
  template's `controlPlaneRef` / `infrastructureRef` switch to them when
  `aks` is set; `AzureCluster` + `KubeadmControlPlane` +
  `KubeadmConfigTemplate` + `AzureMachineTemplate` + `MachineDeployment`
  render only when it is not.
- **`AzureClusterIdentity`** gains a ServicePrincipal branch:
  `type: ServicePrincipal`, `clientID: {{ .Values.azure.clientID }}`,
  `clientSecret: {name: cloud-credentials, namespace: <ns>}` (key
  `clientSecret`) — instead of the WorkloadIdentity/UAMI shape.
- **`MachinePool` + `AzureManagedMachinePool`** per node-group: `mode: System`
  for the first, `User` for the rest; `sku`, `osDiskSizeGB`,
  `scaling: {minSize, maxSize}`, taints/labels passthrough.
- `values.example.aks.yaml` and a helm-unittest suite mirroring
  `charts/aws/tests/eks_test.yaml`.

Until the chart PR lands, `cluster bootstrap` with `aks: true` renders values
the chart ignores — pin the kubeaid fork version to a release that contains the
AKS chart support before using this end-to-end.

## Error handling

- Config validation failures (self-managed-only fields with `aks: true`, DR
  block, bad pool names, zero node-groups) fail `config generate` /
  `cluster bootstrap` fast, before any Azure call.
- `cluster upgrade` / `cluster recover` on an AKS cluster: clear, actionable
  error — never a silent no-op.

## Testing

- Unit tests for the new validation rules (`pkg/config/parser/validate_test.go`)
  and addon-set selection.
- Golden/e2e tests for the new prompt branch and `general.yaml` rendering
  (`e2e/prompt/prompt_test.go`).
- **End-to-end against a real Azure subscription is an open item**, same as the
  real-AWS EKS e2e.

## Out of scope (deliberate)

- Workload identity on AKS (AKS OIDC issuer + azure-workload-identity) — needs
  its own design; DR on AKS (decision 10) inherits whatever it decides.
- `cluster upgrade` / `cluster recover` for AKS (decision 7).
- Azure CNI powered by Cilium (`networkDataplane: cilium`) as an alternative to
  BYO CNI — revisit if operating our own Cilium on AKS proves painful.
- Windows agent pools, AKS local accounts hardening, private clusters.
