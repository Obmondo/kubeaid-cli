# Workload Identity on AKS Clusters — Design

**Date:** 2026-08-11
**Status:** Proposed
**Repos affected:** `Obmondo/kubeaid` (crossplane-compositions + capi-cluster charts),
`Obmondo/kubeaid-cli` (bootstrap flow)
**Depends on:** AKS support (`2026-08-11-aks-support-design.md`, shipped)

## Goal

Give AKS clusters **secretless Azure access**: platform components (Velero for
disaster recovery, eventually CAPZ itself) and customer workloads exchange
their Kubernetes ServiceAccount tokens for Entra ID tokens through federated
identity credentials, using **AKS's managed OIDC issuer**. This unblocks DR on
AKS (validation currently rejects `cloud.disasterRecovery` there) and lets the
CAPZ ServicePrincipal client secret be rotated out of the cluster.

## Background

Self-managed Azure clusters already run workload identity — but they must
**host the OIDC issuer themselves**: an operator-supplied RSA keypair signs the
ServiceAccount tokens, the JWKS + discovery documents are uploaded to a public
storage-account blob, and Crossplane provisions the UAMIs + federated
credentials (`workload-identity-infrastructure` composition: ResourceGroup,
storage Account, `oidc-provider` Container, `capi` UserAssignedIdentity,
RoleAssignments, `capi-capz` / `capi-aso` FederatedIdentityCredentials).

AKS collapses the hard half of that: `oidcIssuerProfile.enabled: true` (set
since the AKS chart support) makes AKS publish the issuer + JWKS itself, and
`securityProfile.workloadIdentity` runs the mutating webhook as a managed
add-on. What remains is only the IAM half — UAMIs + federated credentials —
which the existing composition already models.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | **Reuse and parameterize the existing Crossplane composition** — no CLI/SDK identity plumbing | The `workload-identity-infrastructure` XRD gains an optional `issuerURL` input. When set (AKS), the issuer-hosting resources (storage Account, `oidc-provider` Container, the storage-blob RoleAssignment) are skipped and the FederatedIdentityCredentials point at the given issuer. When unset (self-managed), behavior is unchanged. One declarative owner for Azure IAM across both flavours. |
| 2 | The issuer is **AKS's managed one** | kubeaid-cli reads `AzureManagedControlPlane.status.oidcIssuerProfile.issuerURL` after the control plane is ready and feeds it to the XR. No storage account, no key ceremony, no operator-supplied keypair — `cloud.azure.workloadIdentity` (the keypair block) stays **rejected** for AKS. |
| 3 | The webhook is the **AKS-managed add-on**: `securityProfile.workloadIdentity.enabled: true` on the `AzureManagedControlPlane` | One line in the capi-cluster azure chart's AKS branch; the azure-workload-identity-webhook chart is never deployed on AKS. |
| 4 | Timing: **day-2, in-cluster, post-pivot** | Bootstrap is untouched — CAPZ authenticates with the ServicePrincipal secret as shipped. On self-managed clusters the issuer must exist *before* CAPZ can authenticate (hence the K3D-phase Crossplane run); on AKS that chicken-and-egg doesn't exist, so Crossplane installs on the provisioned cluster itself, GitOps-managed, after the pivot. |
| 5 | Consumers migrate in dependency order: **(a) Velero, (b) CAPZ, (c) documented pattern for customer workloads** | (a) A `velero` UAMI + federated credential (mirroring the `disaster-recovery-infrastructure` composition) unblocks DR on AKS — the validation rejection is lifted in the same change. (b) The chart's `AzureClusterIdentity` flips `ServicePrincipal` → `WorkloadIdentity` for AKS once the `capi` UAMI's federated credentials exist, and the client secret can be rotated out. (c) Customer workloads follow the standard label + federated-credential pattern against the same issuer. |

## Flow changes (kubeaid-cli)

The azure AKS branch of `SetupCluster` (which today skips the whole
Crossplane block) gains a **post-pivot** phase on the main cluster:

1. Install Crossplane + the Azure provider/compositions (same machinery as
   self-managed, different cluster and different moment).
2. Read `AzureManagedControlPlane.status.oidcIssuerProfile.issuerURL` via the
   main-cluster client; fail loudly if absent (the chart enables the profile,
   so absence means a CAPZ/AKS problem).
3. Render the `WorkloadIdentityInfrastructure` XR with `issuerURL` set (and
   without `storageAccountName` / `aadApplicationPrincipalID`, which only feed
   the hosting + storage-role resources), wait for Ready.
4. Only after the XR is Ready: flip the capi-cluster values to
   `azure.workloadIdentity: true` (chart switches the `AzureClusterIdentity`
   type) via the normal kubeaid-config PR flow. Never flip before the
   federated credentials exist — CAPZ would lock itself out.

`validateAzureAKSConfig` drops the `cloud.disasterRecovery` rejection once the
Velero UAMI path (decision 5a) lands — DR provisioning re-uses the
`disaster-recovery-infrastructure` composition with the same `issuerURL`
parameterization.

## Chart changes (KubeAid)

- **crossplane-compositions/charts/azure/workload-identity-infrastructure**:
  XRD gains `issuerURL` (optional string); the composition gates
  ResourceGroup / storage Account / Container / storage RoleAssignment on
  `issuerURL` being empty, and the FederatedIdentityCredentials use
  `issuerURL | default <storage-account issuer URL>`. Same treatment for
  `disaster-recovery-infrastructure` (Velero UAMI + federated credential).
- **capi-cluster/charts/azure**: `securityProfile.workloadIdentity.enabled:
  true` in the `AzureManagedControlPlane` template; a new
  `azure.workloadIdentity` values flag flips the `AzureClusterIdentity` AKS
  branch from `ServicePrincipal` to `WorkloadIdentity` (+ the `capi` UAMI
  client id, surfaced from the XR's connection details).
- helm-unittest coverage for both (the new CI runs them on every PR).

## Error handling

- Missing issuer URL, XR not reaching Ready, or missing federated credentials
  before the identity flip: clear, actionable errors — the identity flip is
  the one step that can lock CAPZ out, so it is strictly gated.
- Crossplane's own Azure provider keeps authenticating with the sealed
  ServicePrincipal secret (as on self-managed); migrating Crossplane itself to
  workload identity is possible later but deliberately not part of this design.

## Testing

- helm-unittest for the composition gating and the chart flags.
- Real-Azure e2e: bootstrap AKS → verify XR Ready → verify a labeled test pod
  gets a token for the `capi` UAMI → flip the identity → delete the client
  secret → confirm CAPZ still reconciles.

## Out of scope (deliberate)

- Migrating self-managed clusters onto a managed-style issuer.
- A customer-facing XR API for per-workload identities (they can create
  `FederatedIdentityCredential` objects directly through the same Crossplane
  provider).
- Automated SP client-secret rotation/removal — operator-triggered after the
  CAPZ flip is verified.

## Open items to resolve during implementation

1. **Exact SA subjects on AKS** for the `capi-capz` / `capi-aso` federated
   credentials (`system:serviceaccount:<ns>:<name>` — verify the CAPZ and ASO
   ServiceAccount names/namespace as deployed by the cluster-api-operator).
2. **Velero role scope**: subscription-level Contributor (current self-managed
   shape) vs. resource-group-scoped — prefer narrowing while we're here.
3. Whether the XR should also emit the `capi` UAMI clientID as a connection
   secret the CLI can template into the chart values, vs. the CLI reading it
   from ARM.
