# Day-2 `cluster sync` for HCloud instance types, via MachineTemplate name rotation

Status: implemented (kubeaid-cli + kubeaid `capi-cluster` chart).
Supersedes the "Day-2 `cluster sync` for cloud instance-type changes" plan in
`docs/TODO.md`, which proposed deleting and recreating the MachineTemplate.

## The problem

Editing `cloud.hetzner.controlPlane.hcloud.machineType` (or `replicas`) in
general.yaml had no effect on a running cluster. Only `bootstrap` re-renders
`values-capi-cluster.yaml`; `cluster sync` refused every non-bare-metal provider,
and `cluster upgrade` only patched `.global.kubernetes.version` plus the machine
image.

## Why delete + recreate was the wrong fix

The original plan was to delete the immutable MachineTemplate and recreate it
with the new `instanceType`, since its spec cannot be updated in place.

That does not work, and fails silently. ClusterAPI decides whether a Machine is
up to date by comparing the **name** of the template it was cloned from against
the name its owner currently references. It never compares the template's
contents:

- `controlplane/kubeadm/internal/filters.go:143` — `matchesTemplateClonedFrom`
  compares the `cluster.x-k8s.io/cloned-from-name` annotation on the infra machine
  against `kcp.Spec.MachineTemplate.Spec.InfrastructureRef.Name`.
- `internal/controllers/machinedeployment/mdutil/util.go:408` —
  `MachineTemplateUpToDate` compares `Spec.InfrastructureRef` (kind + name).

(both `sigs.k8s.io/cluster-api v1.11.10`, the version this repo pins)

So recreating `<cluster>-control-plane` with a different `type:` leaves
`clonedFromName` matching. Every Machine is judged up to date, **nothing rolls**,
and the new instance type applies only to machines created later — a control
plane with mixed instance types and no error reported anywhere.

The existing `cluster upgrade` gets away with the same delete+recreate for the
machine *image* only because the accompanying Kubernetes version change is what
triggers the rollout; the new image just rides along on it.

Worse, deleting a template that a KubeadmControlPlane references opens a window
where the controller cannot clone new machines at all.

## The fix: rotate the name

CAPI's own diagnostic — *"Infrastructure template on KCP rotated from X to Y"* —
names the sanctioned mechanism. Name the template after a hash of its spec:

    <cluster>-control-plane-<sha256(spec)[:8]>
    <cluster>-<nodegroup>-<sha256(spec)[:8]>

Then the two day-2 changes fall out with the right semantics, through plain
GitOps, with no client-go surgery and no window where the template is missing:

| change | template name | what ClusterAPI does |
| --- | --- | --- |
| `replicas` 1 → 3 | unchanged | KubeadmControlPlane scales out. **New members join etcd; nothing is replaced.** |
| `machineType` cpx32 → cpx22 | rotates | Rolling replacement: surge one machine, wait for it to join, remove an old one. |
| node-group `machineType` | that group's rotates | Only that MachineDeployment rolls. |

ArgoCD creates the newly-named template and repoints the owner's
`infrastructureRef` at it in the same sync.

### Chart changes (kubeaid)

`argocd-helm-charts/capi-cluster/charts/hetzner/`:

- `_helpers.tpl` gains `hetzner.hcloud{ControlPlane,NodeGroup}MachineTemplate{Spec,Name}`.
  The **spec** helpers are the single definition of each template's
  `spec.template.spec`; `HCloudMachineTemplate.yaml` renders them and the **name**
  helpers hash them. One definition, so the hash cannot drift from the spec it names.
- `KubeadmControlPlane.yaml` and `MachineDeployment.yaml` take their
  `infrastructureRef.name` from the same name helpers.

Gated on `global.machineTemplateRotation`, default `false`. Enabling it renames
the template even when the spec is unchanged, which rolls the control plane once
— existing clusters must opt in deliberately.

Rotated templates carry `argocd.argoproj.io/sync-options: Prune=false`. A
superseded template is still referenced by the MachineSet being scaled down;
pruning it mid-rollout would strand that MachineSet if it had to replace a
Machine. Stale templates hold no resources and can be deleted once every Machine
has moved off them.

Bare-metal templates keep their fixed names: bare-metal workers are `Machine`
objects, not MachineDeployments, so nothing rotates.

### CLI changes (kubeaid-cli)

- `cluster.machineTemplateRotation` in general.yaml → `global.machineTemplateRotation`
  in the rendered values.
- `cluster sync` now handles Hetzner: re-render `values-capi-cluster.yaml` (plus the
  verbatim `kubeaid-cli.general.yaml` copy), show an inline unified diff, open the PR,
  sync the `capi-cluster` ArgoCD app, and wait on `WaitForControlPlaneRolloutComplete`.
- `cluster upgrade` skips `UpdateMachineTemplate` on rotation-enabled clusters — under
  rotation there is no `<cluster>-control-plane` object to delete — and syncs the whole
  app instead, so the newly-named template lands.

## Gates

`cluster sync` and `cluster upgrade` both mutate a live cluster, and nothing in
kubeaid-cli ever selects a kubeconfig context: every client is built with
`clientcmd.BuildConfigFromFlags("", path)`, silently following whatever
`current-context` the kubeconfig carries.

So both commands now resolve that context and make the operator confirm it —
cluster name from general.yaml, context name, API server, kubeconfig path, and a
loud `MISMATCH` line when the context's cluster name differs from general.yaml's.
Declining, a missing kubeconfig, or no TTY all abort. A command that rolls
control-plane machines should stop, not guess.

`planCapiValuesSync` then inspects the diff and **refuses** two changes outright,
because both fail silently:

1. **A rotating change while `machineTemplateRotation` is false.** Nothing would
   roll (see above). Told to enable rotation first.
2. **A rotating change combined with a replicas change.** That asks ClusterAPI to
   both add members and replace them; starting from one control-plane node it
   walks etcd through a sequence of membership changes with no quorum to lose.
   Told to split it into two syncs.

And **warns** when a rolling change would run against fewer than 3 control-plane
replicas: a rolling replacement surges one machine, waits for it to join etcd, then
removes an old one — below three replicas every intermediate membership is a single
node away from losing quorum.

## Applying this to `netbird-obmondo-com` (1 × cpx32 → 3 × cpx22)

The refusals above force the correct order. Two separate syncs:

**Phase 1 — scale out.** Set `replicas: 3`, leave `machineType: cpx32`, leave
`machineTemplateRotation: false`. The template spec is untouched, so its name is
untouched: the KubeadmControlPlane simply scales, and two new control-plane
members join etcd. Nothing is replaced. Wait for all three to be Ready.

**Phase 2 — re-type.** Set `machineTemplateRotation: true` and
`machineType: cpx22`. The template rotates, and ClusterAPI rolls the three
control-plane machines one at a time with quorum held throughout.

Doing both in one sync is refused.

Before phase 2, confirm `cpx22` is actually large enough for a control plane
running etcd — this is a downsize. `GetVMSpecs` resolves the type against the live
Hetzner API, so an unknown type fails at render time rather than silently.

## Follow-ups

- AWS and Azure name their MachineTemplates the same fixed way
  (`charts/aws/templates/AWSMachineTemplate.yaml:4`,
  `charts/azure/templates/AzureMachineTemplate.yaml:4`), so they have the same
  silent no-op. `cluster sync` still refuses them. Porting the helpers is
  mechanical.
- `cluster upgrade` has a PR-merge gate but still no inline diff.
- The `capi-cluster` chart's helm-unittest suites are not wired into CI
  (no `helm unittest` invocation anywhere in the kubeaid repo), so
  `tests/machine-template-rotation_test.yaml` only runs when someone runs it.
