# EKS Support in KubeAid CLI — Design

**Date:** 2026-08-09
**Status:** Implemented (2026-08-10) — see "Implementation notes" at the bottom for
deltas discovered while building
**Repos affected:** `Obmondo/kubeaid-cli`, `Obmondo/kubeaid` (the `capi-cluster` Helm chart)

## Goal

Let `kubeaid-cli` provision and manage **Amazon EKS** clusters (AWS-managed control
plane) alongside the existing self-managed CAPA-based AWS clusters, using the same
GitOps flow: throwaway K3D management cluster → Cluster API provisions the target →
`clusterctl move` pivots → ArgoCD reconciles the KubeAid addon stack.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | EKS is a **variant of the existing AWS provider**, not a sixth cloud provider | The infrastructure provider stays CAPA (`aws`); credentials, IAM stack, and DR are shared. A separate provider would duplicate all of it and break the `getInfrastructureProviderName()` == provider-name assumption. |
| 2 | Workers are **self-managed `MachineDeployment`s attached to the EKS control plane** — not `AWSManagedMachinePool` | Upstream CAPA v2.11.1 classes EKS support as **Stable** but machine pools (incl. managed) as **experimental**. The stable path also reuses KubeAid's existing failure-domain MachineDeployment fan-out and CAPI-mode cluster-autoscaler. Managed machine pools can be added later when CAPA graduates them. |
| 3 | Node bootstrap uses **`NodeadmConfigTemplate`** (AL2023); **EKS clusters require Kubernetes ≥ v1.33** | Upstream: `EKSConfig`/AL2 only supports k8s ≤ v1.32 and AL2 went EOL 2026-06-30. Single bootstrap provider keeps the surface small. |
| 4 | Worker AMIs are resolved by CAPA via `ami.eksLookupType: AmazonLinux2023` | No AMI prompt or Canonical simplestreams lookup for EKS. `insecureSkipSecretsManager: true` is set on the machine template, as upstream requires for AL2023. |
| 5 | **Cilium is the default (and only) CNI** on EKS clusters | Stack uniformity across all KubeAid clusters (network policy, Hubble). Implemented declaratively: `AWSManagedControlPlane.spec.vpcCni.disable: true` and `spec.kubeProxy.disable: true`; Cilium runs with kube-proxy replacement against the EKS endpoint on port 443. |
| 6 | The prompt asks **self-managed vs EKS before credentials** | Credentials are identical either way (CAPA admin bootstrap key), but the questions after them differ: EKS has no HA/replica question (AWS runs the CP multi-AZ), no AMI question (decision 4), and no SSH key (workers stay keyless; Session Manager covers debugging). |
| 7 | Lifecycle scope: **bootstrap + delete** for EKS. `cluster upgrade` is **KubeOne-only** going forward. `cluster recover` is **out of scope** for this design | Once pivoted, the cluster is self-managing: an upgrade is a GitOps edit (bump the version in `values-capi-cluster.yaml`; ArgoCD syncs; CAPA upgrades the EKS control plane and rolls the MachineDeployments). Recover is conceptually needed for *all* cloud providers but is not fully supported yet — deliberately deferred, not forgotten. |
| 8 | **No IAM changes needed** | Because workers are MachineDeployments (decision 2), the default CAPA CloudFormation stack already contains everything EKS needs: CAPA's defaulter ships with `EKS.Disable: false`, so `eks-controlplane.cluster-api-provider-aws.sigs.k8s.io` (role) and `controllers-eks.…` (policy) exist in every stack kubeaid-cli creates today. Managed machine pools would have needed a custom `AWSIAMConfiguration`; we don't. |

## Configuration surface

### `general.yaml`

```yaml
cloud:
  aws:
    region: eu-west-1
    eks: true            # NEW — selects the managed control plane
    nodeGroups:          # unchanged shape
      - name: primary
        instanceType: m5.large
        minSize: 3
        maxSize: 9
```

Validation when `eks: true`:

- `sshKeyName`, `controlPlane.*` (replicas, instanceType, ami, loadBalancerScheme)
  are **rejected** with a clear error — they have no meaning on EKS.
- Kubernetes version must be ≥ v1.33 (decision 3), on top of the existing
  released/non-EOL checks.

`secrets.yaml` is unchanged: same admin access key ID / secret / optional session
token, exported the same way, feeding the same embedded clusterawsadm calls.

### Prompt flow (`pkg/config/prompt/provider_aws.go`)

```
provider = AWS
  → NEW select: "Control plane: self-managed (EC2) / EKS (managed by AWS)"
  → credentials form                     (unchanged)
  → [self-managed] HA confirm → AMI discovery → SSH key derivation   (unchanged)
  → [EKS]          nothing — straight to node groups
```

The select writes a new `PromptedConfig` field (e.g. `AWSEKS bool`) consumed by
`pkg/render/templates/general.yaml.tmpl`, which gains an EKS branch. `postProcess`
(SSH-key derivation) is skipped for EKS.

## Provisioning flow changes (kubeaid-cli)

Everything below is a delta on `provisionMainClusterUsingClusterAPI`; the K3D
management cluster, ArgoCD install, kubeaid-config templating, PR-merge gate, and
`clusterctl move` pivot are untouched.

1. **CAPI providers** — `values-cluster-api-operator.yaml.tmpl` currently
   hard-codes `bootstrap: kubeadm` / `controlPlane: kubeadm`. When EKS: also
   declare the `eks` **bootstrap** and **control-plane** providers (CAPA ships
   them as separate operator components). The kubeadm providers remain — the K3D
   management cluster itself needs nothing new, and `clusterctl move` back out
   (delete path) requires the same providers in the management cluster.
2. **Addon set** (`pkg/constants/templates.go` selection in
   `getEmbeddedNonSecretTemplateNames`):
   - drop `ccm-aws` — the EKS control plane runs the AWS cloud controller;
   - keep `cluster-autoscaler` — CAPI mode works unchanged against
     MachineDeployments;
   - keep `external-snapshotter`;
   - Cilium values gain an EKS branch: `k8sServicePort: 443` and the
     `AWSManagedControlPlane` endpoint host instead of `6443`/KCP endpoint.
     The initial Cilium install cannot ride the KubeadmControlPlane postKubeadm
     hook (there is no KCP) — it is installed the same way but triggered after
     the control plane reports ready.
3. **Control-plane gates** — skip `WaitForControlPlaneRolloutComplete`'s KCP
   summary (it already degrades gracefully on missing KCP) and skip
   `RemoveNoScheduleTaintsFromMasterNodes` (no control-plane nodes exist).
4. **Pivot** — the pre-pivot CAPA credential-zeroing and rollout applies
   unchanged; `clusterctl move` supports managed control planes.
5. **Guards** — `cluster upgrade` (any CAPI provider) and `cluster recover`
   (EKS) exit with a message pointing at the GitOps flow / roadmap
   respectively.

## Chart changes (KubeAid repo, `argocd-helm-charts/capi-cluster`)

Behind a new `aws.eks` values flag in `charts/aws/`:

- **`AWSManagedControlPlane`** template: version, region, network,
  `bastion.enabled`, `vpcCni.disable: true`, `kubeProxy.disable: true`,
  endpoint access config. The `Cluster` template's `controlPlaneRef` switches
  to it when `eks` is set; `AWSCluster` + `KubeadmControlPlane` +
  `KubeadmConfigTemplate` render only when it is not.
- **`NodeadmConfigTemplate`** template; the existing `MachineDeployment`
  failure-domain fan-out is reused with its bootstrap `configRef` switched to
  it, and `AWSMachineTemplate` gains the EKS branch:
  `ami.eksLookupType: AmazonLinux2023`, `insecureSkipSecretsManager: true`,
  no `sshKeyName`.
- **`provider-aws.yaml`**: add `EKS: true` (and `EKSEnableIAM` if needed) to
  `featureGates`, following the existing `MachinePool` gate pattern.
- Chart unit tests for the EKS rendering path, mirroring
  `charts/hetzner/tests/`.

## Error handling

- Config validation failures (CP fields with `eks: true`, version < v1.33) fail
  `config generate` / `cluster bootstrap` fast, before any AWS call.
- `cluster upgrade` / `cluster recover` on an EKS cluster: clear, actionable
  error (what to edit in the kubeaid-config repo for upgrades; recover roadmap
  pointer) — never a silent no-op.
- Bootstrap waits: control-plane readiness is judged from
  `AWSManagedControlPlane` status conditions instead of KCP rollout state.

## Testing

- **kubeaid-cli**: golden tests for the new prompt branch and `general.yaml`
  rendering (`pkg/render/render_golden_test.go`,
  `pkg/config/prompt/render_hybrid_test.go`, `e2e/prompt/prompt_test.go`);
  unit tests for the new validation rules and addon-set selection.
- **KubeAid chart**: `helm unittest` cases for EKS on/off rendering.
- **End-to-end**: a real EKS bootstrap + delete against a test AWS account,
  exercised manually first; CI automation is follow-up work.

## Out of scope (deliberate)

- `AWSManagedMachinePool` (EKS managed node groups) and Fargate — experimental
  upstream; revisit when CAPA graduates them.
- `cluster recover` for EKS — recover needs a design of its own covering all
  cloud providers; today it is not fully supported anywhere.
- IRSA / Pod Identity for workload IAM (kube2iam replacement) — tracked
  separately; DR on EKS inherits whatever that effort decides.
- Reusing an existing VPC (`vpcID`) with EKS — parity item; verify CAPA
  `AWSManagedControlPlane` network-spec support during implementation and gate
  accordingly.

## Open items to resolve during implementation

1. **EBS CSI driver**: EKS does not install a storage driver by default. The
   KubeAid repo already carries `argocd-helm-charts/aws-ebs-csi-driver`; add it
   to the EKS addon set in kubeaid-cli (a values template + registration in
   `pkg/constants/templates.go` — the CLI does not template it for self-managed
   AWS today either). Settle where its IAM permissions come from (node instance
   profile vs IRSA) during implementation.
2. **Cilium initial install trigger**: exact hook point replacing the KCP
   postKubeadm hook (control-plane-ready wait vs ArgoCD sync wave).
3. **`EKSEnableIAM` feature gate**: whether CAPA needs it for our resource
   shapes, or `EKS: true` alone suffices.

## Implementation notes (deltas from the design above)

- **No cluster-api-operator changes were needed.** Since CAPA v2 the EKS
  bootstrap and control-plane providers are merged into the main AWS provider
  and EKS support is enabled by default ("Support for EKS is enabled by
  default when you use the AWS infrastructure provider" — CAPA
  eks/enabling.md). The chart still sets `EKS: true` explicitly on the
  `InfrastructureProvider` feature gates, as insurance against a future
  default flip.
- **No IAM changes were needed**, as predicted by decision 8.
- **Initial Cilium install**: kubeaid-cli installs the KubeAid cilium chart
  into the EKS cluster itself (`installCiliumOnEKSCluster`,
  `pkg/core/bootstrap_cluster.go`) via the same Helm machinery used for
  ArgoCD, with `k8sServiceHost`/`k8sServicePort` taken from the cluster
  kubeconfig — replacing the KubeadmControlPlane postKubeadm hook. The cilium
  ArgoCD App adopts the manifests afterwards.
- **Pre-pivot CAPA credential zeroing is skipped for EKS**: post-pivot CAPA
  runs on workers whose `nodes.…` instance profile lacks the controllers
  policy, so CAPA keeps using the cloud-credentials Secret (IRSA later). For
  the same reason the chart drops the control-plane nodeSelector on the CAPA
  deployment when `aws.eks` is set.
- **`cluster upgrade` is now KubeOne-oriented**: the AWS path errors out for
  EKS clusters, pointing at the GitOps flow (bump
  `global.kubernetes.version` in `values-capi-cluster.yaml`). `cluster
  recover` errors out for EKS clusters.
- **values-cilium.yaml now renders the endpoint port from the kubeconfig**
  (`ControlPlaneEndpointPort`, default 6443 — EKS yields 443) instead of a
  hard-coded 6443.
- **KubeAid chart**: `capi-cluster/charts/aws` gained `eks` values flag,
  `AWSManagedControlPlane` + `AWSManagedCluster` + `NodeadmConfigTemplate`
  templates, EKS branches in `AWSMachineTemplate`/`MachineDeployment`,
  kubeadm resources gated off, `MachineHealthCheck` (control-plane-scoped)
  gated off, machine pools excluded for EKS, plus
  `values.example.eks.yaml` and a helm-unittest suite
  (`charts/aws/tests/eks_test.yaml`, 10 tests, passing).
- **Deployed CAPA version caveat**: the chart's `global.capa.version` default
  is v2.9.2; NodeadmConfig needs a newer CAPA — bump to >= v2.11 when
  enabling EKS.
