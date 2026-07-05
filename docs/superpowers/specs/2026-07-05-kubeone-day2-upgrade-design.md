# KubeOne day-2 upgrades via `kubeaid-cli cluster upgrade`

**Date:** 2026-07-05
**Status:** Approved (brainstorming session with Ashish)

## Problem

The `bare-metal` provider provisions clusters with KubeOne (embedded
`k8c.io/kubeone` library, `kubeone apply` run in-process), but kubeaid-cli
only owns bootstrap and delete for it. Every day-2 change — most urgently
Kubernetes version upgrades — means hand-editing the rendered KubeOne
manifest and running the raw `kubeone` binary. Additionally, the embedded
KubeOne v1.12 caps supported Kubernetes at v1.34, and clusters need to go
beyond that.

## Goal

`kubeaid-cli cluster upgrade` — bare command, no provider subcommand, no
flags — upgrades the cluster's Kubernetes version. Provider and target
version are both read from `general.yaml`:

- provider: auto-detected from which `cloud.*` section is set (already
  done at config-parse time, `pkg/config/parser/parse.go`)
- target version: `cluster.k8sVersion`

Flow for the operator: edit `k8sVersion` in `general.yaml` → run
`kubeaid-cli cluster upgrade` → done.

## Leg 1 — dependency bump: k8c.io/kubeone v1.12.0 → v1.13.5

- KubeOne v1.13 supports Kubernetes v1.33–v1.35 (v1.31/v1.32 support
  removed upstream). v1.13.5 is the latest stable patch; v1.14 is alpha.
- `MinKubeOneSupportedK8sVersion` → `v1.33`,
  `MaxKubeOneSupportedK8sVersion` → `v1.35`
  (`pkg/constants/constants.go`). Parse-time range validation for the
  bare-metal provider picks these up unchanged.
- Usage surfaces to verify after the bump (compile + behavior):
  - embedded `pkg/cmd` root — bootstrap's
    `apply --manifest … --auto-approve --force-install` args
  - `pkg/ssh` + `pkg/executor` — Hetzner SSH pool,
    `pkg/utils/commandexecutor/ssh.go`, bare-metal host validation
  - `kubeone.k8c.io/v1beta2` manifest template — unchanged upstream
- Transitive risk: `k8s.io/*` is replace-pinned to v0.33.6, which is the
  library line KubeOne v1.13 targets; argo-cd v2 / cluster-api /
  controller-runtime pins must continue to resolve. Gate:
  `go build ./... && go test ./...`.
- Cilium chart pin (`1.19.1` in `kubeone-cluster.yaml.tmpl`) checked
  against k8s v1.35; bumped only if incompatible.
- **Operator caveats (documented):** KubeOne v1.13 cannot manage clusters
  below v1.33 — a cluster still on v1.32 needs one manual hop to v1.33
  with a KubeOne v1.12 binary before this kubeaid-cli version can take
  over. Node upgrades also move containerd 1.7.x → 2.x.

## Leg 2 — bare `cluster upgrade`, config-driven

`UpgradeCmd` (cmd/kubeaid-core/root/cluster/upgrade/upgrade.go) gets its
own `Run` — same provider-agnostic pattern as `cluster bootstrap`.
Dispatch on `globals.CloudProviderName`:

| provider | behavior |
|---|---|
| `bare-metal` | new `core.UpgradeClusterUsingKubeOne` (below) |
| `aws` / `azure` / `hetzner` | existing `core.UpgradeCluster`; `NewKubernetesVersion` = `cluster.k8sVersion`, machine images sourced from the provider's own `general.yaml` section (AWS control-plane `ami.id`, Azure `canonicalUbuntuImage.offer`, Hetzner `hcloud.imageName` / `bareMetal.installImage.imagePath`) |
| `local` | clear error — dev cluster, recreate instead |

Already at target version → friendly no-op, exit 0 (bare-metal path).

### `core.UpgradeClusterUsingKubeOne` (pkg/core)

Sibling of the CAPI `UpgradeCluster`, not threaded through it:

1. **Pre-flight**
   - target = `cluster.k8sVersion` (range-validated at parse time)
   - current = live cluster (kubeconfig from bootstrap's output path);
     fallback = `versions.kubernetes` in the rendered manifest, with a
     warning
   - hop rule: same-minor patch bump or exactly +1 minor; refuse
     downgrades and multi-minor jumps with a message to step through
     (e.g. v1.33 → v1.34 → v1.35)
   - best-effort all-nodes-Ready check when the cluster is reachable
2. **Render + push** — re-render `kubeone/kubeone-cluster.yaml` from
   `general.yaml` (existing `createOrUpdateKubeOneConfigFile`) into the
   kubeaid-config clone; commit + push via the existing PR workflow
   (branch) or directly with `--skip-pr-workflow`. Reconcile semantics: other
   pending `general.yaml` changes (e.g. hosts) ride along — general.yaml
   is the source of truth. Documented loudly.
3. **Apply** — embedded
   `kubeone apply --manifest <rendered> --auto-approve` in-process (no
   `--force-install`). KubeOne performs the rolling upgrade itself:
   control plane first, then static workers. Idempotent — a failed run is
   resumed by re-running the command.
4. **Post-verify** — poll until all nodes are Ready at the target kubelet
   version (bounded wait); print a summary.

### CLI compatibility (revised during implementation, per Ashish)

Fully GitOps, on purpose: the `aws`/`azure`/`hetzner` subcommands and
every upgrade flag (`--new-k8s-version`, `--ami-id`,
`--new-image-offer`, `--new-image-name`, `--new-image-path`) are
REMOVED. `general.yaml` already owns all of those inputs. The only
remaining flag is `--skip-pr-workflow`. This intentionally breaks any
script that used the old flags — the replacement is a config edit plus
`kubeaid-cli cluster upgrade`.

## Error handling

- Version/hop violations and unsupported ranges fail pre-flight with
  actionable messages (repo's assert style).
- `kubeone apply` failure: the manifest is already pushed; git is ahead
  of the cluster. Re-running `cluster upgrade` resumes — kubeone apply
  converges live state to the manifest.

## Testing

- Table-driven unit tests for the hop-validation helper.
- Template render check at v1.35.
- Real e2e needs SSH-able hosts — out of CI scope; staging-cluster
  verification.

## Docs

- README capability matrix: Bare Metal → Upgrade ✓
- new `docs/upgrade-bare-metal.md`: config-driven walkthrough, multi-hop
  stepping, containerd 2.x note, v1.32 → v1.33 old-binary caveat
- `docs/architecture.md`: provider table / upgrade flow touch-ups

## Out of scope

- Node add/remove UX and `recover` for bare-metal (the re-render+apply
  core is reusable for those later)
- Auto-walking multi-minor upgrades
- Shelling out to a system kubeone binary (two-versions-in-play problem;
  embedded library stays)
