# Upgrading a bare-metal (KubeOne) cluster

> Kubernetes version upgrades for clusters provisioned with the generic
> `bare-metal` provider. No flags, no hand-run `kubeone` binary:
> `general.yaml` is the source of truth, and kubeaid-cli drives the
> embedded KubeOne.

Supported Kubernetes range: **v1.33 – v1.35** (KubeOne v1.13). The range
moves when kubeaid-cli bumps its embedded KubeOne.

## Steps

1. Bump the Kubernetes version in your cluster's `general.yaml`:

   ```yaml
   cluster:
     name: demo
     k8sVersion: v1.35.2
   ```

   One minor version at a time — kubeadm (and therefore KubeOne) cannot
   skip minors. Patch-only bumps within the same minor are fine too.

2. Run the upgrade:

   ```bash
   kubeaid-cli cluster upgrade
   ```

   The provider is auto-detected from `general.yaml`, so there's no
   `bare-metal` subcommand or version flag. What happens:

   1. **Pre-flight** — the current version is read from the live cluster
      (lowest kubelet version across nodes; falls back to the rendered
      KubeOne manifest in kubeaid-config when the cluster isn't
      reachable). The run refuses downgrades, minor-skips, and NotReady
      nodes. When the target is beyond v1.34, every host is additionally
      SSHed into and checked for **cgroup v2** (see below).
   2. **Render + push** — `kubeone/kubeone-cluster.yaml` is re-rendered
      from `general.yaml` and pushed to your kubeaid-config repo. By
      default this goes through the PR workflow (the run waits until you
      merge); pass `--skip-pr-workflow` to push directly to the default
      branch.
   3. **Apply** — the embedded `kubeone apply` performs the rolling
      upgrade: control plane first, then the static workers, one node at
      a time (drain, upgrade, uncordon).
   4. **Verify** — the run waits until every node is Ready at the target
      kubelet version.

3. Multi-minor jumps = repeat. Going v1.33 → v1.35 means editing
   `general.yaml` to v1.34.x, running the upgrade, then editing to
   v1.35.x and running it again.

## Reconcile semantics

The manifest is re-rendered from `general.yaml` in full. Any other
pending change in there (a host you added to a node-group, an SSH port
change, …) rides along into the same `kubeone apply`. Keep unrelated
`general.yaml` edits out of an upgrade if you don't want that.

## Config-only changes: `cluster sync`

For changes that don't bump the Kubernetes version — the bundled helm
releases (e.g. Cilium), addons, a new host in a node-group — run:

```bash
kubeaid-cli cluster sync
```

1. Verifies the cluster already runs `cluster.k8sVersion` (a pending
   version bump is refused — run `cluster upgrade` first).
2. Re-renders and pushes the KubeOne manifest (same PR workflow,
   `--skip-pr-workflow` works here too).
3. Runs a plain `kubeone apply`: KubeOne's steady-state task set
   reconciles helm releases and addons, joins newly added static
   workers, renews soon-to-expire certificates and re-labels nodes.
   It never cordons or drains in-version nodes, so sync is
   non-disruptive and safe to rerun anytime.

One exception: **kubelet tuning** (`cloud.bare-metal.kubelet`) is
rewritten by KubeOne only during its per-node upgrade procedure, which
sync deliberately never forces (it would cordon + drain every node).
Those changes take effect on the next `cluster upgrade`.

## If a run fails

Rerun `kubeaid-cli cluster upgrade`. The flow is idempotent:

- kubeaid-config may be ahead of the cluster (manifest pushed, apply
  failed) — the rerun detects the unchanged manifest and goes straight
  to `kubeone apply`, which converges the remaining nodes.
- The "current version" pre-flight uses the *lowest* kubelet version, so
  a half-upgraded cluster still validates as one hop away from the
  target.

## Single-node clusters

KubeOne cordons and drains each node during the upgrade. On a
single-node cluster an evicted pod has nowhere to reschedule, so any
PodDisruptionBudget selecting running pods deadlocks the drain — even
`maxUnavailable: 1` (the first eviction consumes the budget, the
replacement stays Pending on the cordoned node, and every further
eviction is forbidden).

`kubeaid-cli cluster upgrade` detects this: when the cluster has
exactly one node, every pod-selecting PDB is listed and the run asks
for consent before touching anything. On approval, the PDBs are
removed, kept removed while the drain runs (ArgoCD self-heal
recreates its PDBs within seconds otherwise), and restored afterwards
— ArgoCD-managed PDBs come back via ArgoCD's own sync, the rest are
re-created by the run itself. On decline (or when there's no TTY to
ask on), the upgrade aborts and prints the exact `kubectl delete pdb`
commands to run by hand before retrying.

## cgroup v2 (Kubernetes ≥ v1.35)

Kubernetes v1.35 dropped cgroup v1 support — a kubelet beyond v1.34
refuses to start on a cgroup v1 host. The upgrade pre-flight verifies
every host before touching anything. To check a host yourself:

```bash
stat -fc %T /sys/fs/cgroup   # must print: cgroup2fs
```

Hosts on cgroup v1 need `systemd.unified_cgroup_hierarchy=1` on the
kernel command line (and a reboot) before the upgrade.

## Caveats

- **Clusters still on v1.32 or older**: KubeOne v1.13 (embedded since
  this kubeaid-cli version) can't manage them. Do one manual hop to
  v1.33 with a KubeOne v1.12 binary first, then use `kubeaid-cli
  cluster upgrade` from there on.
- **containerd 1.7 → 2.x**: KubeOne v1.13 moves nodes to containerd 2.x
  as part of node upgrades. This is handled per-node during the rolling
  upgrade; no action needed, but expect it in the diff of installed
  packages.
- After the upgrade, `kubectl get nodes` is the quick sanity check —
  every node Ready, every kubelet at the target version.
