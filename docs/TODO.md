# TODO

Pending engineering work, kept out of the issue tracker while it's still
design-stage. Each item states the problem, the options, and a plan.

## Pending feature work

### Create the mesh DNS zone via the Mgmt API

The remaining NetBird dashboard step in `printNetBirdOperatorInstructions`
(create the mesh DNS zone) is manual because the kubernetes-operator only
*references* the zone (`dnsZoneRef.name`) — it never creates it. The shared
`k8s-<cluster>` group is no longer manual: the netbird-operator chart now
provisions it via a Group CR. Checked 2026-07-03: upstream operator v0.7.0
is current and has no issue/PR for creating DNS zones; re-check before
building this, and drop it if the operator gains support.

Worse, the instructions box only prints when the
`netbird-mgmt-api-key` Secret is missing — on the secrets.yaml path
nobody is told to create the zone at all, and the NetworkRouter /
NetworkResource CRs sit pending until someone does.

Once the API-key gate passes, kubeaid-cli holds an Admin PAT, so it
can create the zone itself. Endpoint verified against Mgmt v0.72.4
(what the kubeaid netbird chart deploys), auth
`Authorization: Token <PAT>`:

- `GET`/`POST /api/dns/zones` — `{"name", "domain",
  "enable_search_domain", "distribution_groups"}`; open question:
  which `distribution_groups` (the `All` group matches a manual
  dashboard setup; scoping to `k8s-<cluster>` is tighter but makes
  name resolution failures confusing)

Plan: an idempotent ensure step (list → create when missing) right
after `awaitNetBirdOperatorToken` returns; on failure warn and print
the manual dashboard step instead of aborting a provisioned cluster.
The group step has already been trimmed from the instructions box now
that the chart creates the Group CR. Separately, a default clusterProxy
RBAC binding `k8s-<cluster>-admins → cluster-admin` when
`clusterProxy.rbac` is empty is still open (decided 2026-07-03:
cluster-admin default is fine — group membership is the policy).

### NetBird operator PAT rotation

NetBird user PATs cap at 180 days; service-user PATs may allow
longer but no official no-expiry. Cluster ops on a 180-day
rotation isn't acceptable long-term. Two options:

1. **In-cluster CronJob** — uses the current PAT to mint a new one
   via NetBird Mgmt's `POST /api/users/<id>/tokens`, patches the
   `netbird-mgmt-api-key` Secret, rolls the operator. Schedule
   every ~5 months. Self-perpetuating once seeded.

2. **Upstream patch** — allow `--no-expiry` (or a much longer cap,
   e.g. 5y) on service-user PATs only. Smaller Mgmt-side change;
   maintainers likely receptive given the automation-friendly
   service-user framing.

Pair (1) into the operator-token gate's chart overlay; pursue (2)
in parallel and drop the CronJob when it lands.

### Cilium components must reach kube-apiserver without DNS

Bug seen on the `netbird-obmondo-com` bootstrap: after
`DisableControlPlaneLBPublicInterface` runs, every Cilium component
(both operator Deployment and agent DaemonSet) crashloops because
its `KUBERNETES_SERVICE_HOST` is the public hostname
(`api.vpn.<cluster>`), which resolves to the now-blackholed public
LB IP via the host's `/etc/resolv.conf` (hostNetwork pods skip
CoreDNS — `dnsPolicy: ClusterFirst` is silently downgraded to
`Default`). Go's HTTP client tries the first IP in the resolution
list and hangs on the TCP blackhole for the full kernel retry
window (~75–127s), so fallback to the LB private IP never fires
within the operator's startup deadline.

Two layered fixes:

1. **In-cluster `KUBERNETES_SERVICE_HOST`** — overlay the cilium
   values to pin `k8sServiceHost` to `{{ .ControlPlaneLBPrivateIP }}`
   instead of the hostname. Bypasses DNS entirely. Simplest, but
   loses the symbolic reference to the cluster endpoint.

2. **`hostAliases` via upstream chart PR** — keep
   `k8sServiceHost: api.vpn.<cluster>` and inject a hostAliases
   entry mapping that hostname to the LB private IP, so
   kubelet-managed `/etc/hosts` resolves it correctly even for
   hostNetwork pods. Requires upstream Cilium PR for
   `extraHostAliases` / `operator.extraHostAliases` values (draft
   prepared in this session's chat, not yet filed).

Pursue (2) upstream; ship (1) as the immediate kubeaid-cli fix in
`values-cilium.yaml.tmpl`. Drop (1) when (2) is released.

### CoreDNS hosts block leaves the disabled public IP

`pkg/core/templates/k8s-configs/coredns.configmap.yaml.tmpl`
emits both the LB's bootstrap public IP and the steady-state
private IP, with the public IP first. After
`DisableControlPlaneLBPublicInterface` runs, the public entry
points at a blackhole, but stays in the ConfigMap forever — every
pod looking up `api.vpn.<cluster>` via CoreDNS still gets the dead
address as the preferred answer.

Two fixes:

- **Reorder** — emit the private IP first. Clients hit it
  instantly; public stays as harmless fallback.
- **Conditional emit** — only include the public IP while the LB
  public interface is still up. After
  `DisableControlPlaneLBPublicInterface` runs, re-render and push
  the ConfigMap without the public line. Cleaner long-term.

Reorder is a one-line change; conditional emit needs the disable
step to trigger a re-render. Land reorder first; build conditional
emit as part of the same flow that re-disables on rerun.

### Default the netbird-operator webhook to `failurePolicy: Ignore`

The upstream chart ships `MutatingWebhookConfiguration` with
`failurePolicy: Fail`. On a single-CP cluster where the operator
itself crashloops (missing API key, cert-manager not yet ready,
etc.), this blocks every cluster-wide Pod create — including the
operator's own rollouts, making it almost impossible to recover
without SSH-into-the-node patches. Overlay
`webhook.failurePolicy: Ignore` in
`values-netbird-operator.yaml.tmpl` so the cluster degrades
gracefully when the operator is unhealthy. Optional sidecar
injection is the worst-case loss; an unwedged cluster is the win.

Belongs with the broader operator-config TODO above, but worth
shipping standalone if that wider work slips.

### Detect `make build` dev versions in `storagectlVersion`

`Makefile:1` injects `VERSION = $(git describe --tags --always --dirty)`
into `cmd/kubeaid-core/root/version.Version`, so a local `make build`
run produces a string like `v0.23.0-54-g0d24247-dirty`. The gate in
`pkg/core/templates.go:255` (`storagectlVersion`) only treats `""` and
`"dev"` as dev, so any Makefile-built kubeaid-cli pins that describe
string into `global.kubeaidStoragectl.version` of the rendered
`values-capi-cluster.yaml`. Result: every commit on main produces a
noise diff + PR in kubeaid-config on every bootstrap run, and the
chart's `latest` fallback (intended for dev) never fires.

Extend the gate to recognise git-describe dev markers:

- suffix `-dirty` → dev (return `""`)
- segment `-g<hex>` (post-tag git-describe form, with or without
  `-dirty`) → dev

Release tags from goreleaser (`{{ .Tag }}` → `v0.23.0`, `v0.23.0-rc.1`)
keep passing through verbatim. Extend `TestStoragectlVersion` with the
new dev cases (`v0.23.0-dirty`, `v0.23.0-54-g0d24247`,
`v0.23.0-54-g0d24247-dirty`) so the regex can't drift silently.

### Pre-flight ArgoCD-rendered Helm values against the chart's schema

A broader-scope follow-up to the Hetzner bare-metal regions fix: a
local pre-flight that runs `helm template --validate` (or
`kubeconform`, or `jsonschema`) against the rendered
`values-capi-cluster.yaml` before kubeaid-cli pushes the kubeaid-config
PR. The bare-metal regions case was caught the hard way (ArgoCD sync
failure) because `go-playground/validator` only checks slice
non-nil-ness on `required` — a Helm schema's `minItems`, `pattern`,
or other JSONSchema constraints aren't enforced on the Go side. A
pre-flight surfaces the failure as a clean field-level error from
kubeaid-cli with the offending path, same shape as the parser's
existing `validate` errors. Defer until we hit the next case from a
different field; the regions one is fixed at source.

### Day-2 `cluster sync` for cloud instance-type changes (AWS / Azure)

hcloud is done — see `docs/specs/2026-07-09-machinetemplate-rotation-day2-sync.md`.

The delete+recreate plan this section used to carry was wrong, and worth recording
why: ClusterAPI decides a Machine is up to date by comparing the *name* of the
template it was cloned from against the name its owner references
(`matchesTemplateClonedFrom` for KCP, `MachineTemplateUpToDate` for MachineDeployments).
It never compares the template's contents. Recreating a template under its old name
therefore rolls nothing, and the new instanceType applies silently to machines
created later. `cluster upgrade` only appeared to work because the Kubernetes version
change was doing the rolling.

The fix shipped for hcloud is to name MachineTemplates after a hash of their spec, so
a changed instanceType rotates the name and CAPI rolls normally — pure GitOps, no
client-go surgery. It is gated on `cluster.machineTemplateRotation` because adopting
it renames the template once, which costs one control-plane roll.

Remaining: AWS and Azure name their MachineTemplates the same fixed way
(`charts/aws/templates/AWSMachineTemplate.yaml:4`,
`charts/azure/templates/AzureMachineTemplate.yaml:4`) and have the same silent no-op.
`cluster sync` still refuses both providers. Porting the `_helpers.tpl` name/spec
helpers is mechanical; the CLI side already generalises (`planCapiValuesSync` reads
only the rendered values, and would need each provider's values paths added).

Also still open: `cluster upgrade` has the PR-merge gate but no inline diff.
