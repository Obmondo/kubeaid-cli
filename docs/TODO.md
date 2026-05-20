# Docs TODO

## Troubleshooting guide

A `docs/troubleshooting.md` should collect the recovery paths for
recurring bootstrap failures so operators don't have to re-discover
them from chat logs. Initial topics:

### Hetzner

- **CAX server type not available in datacenter** (`error during placement (resource_unavailable, ...)`)
  - How to spot in the live status table
  - When it's transient (Hetzner capacity) vs misconfig (wrong type for location)
  - Workaround: update `Cloud.Hetzner.ControlPlane.HCloud.MachineType` and re-run

- **`HCloudMachineTemplate.Spec is immutable` on ArgoCD sync after config change**
  - Why: CAPH treats template specs as immutable
  - Pre-bootstrap recovery: `kubectl delete machine -l cluster.x-k8s.io/cluster-name=<cluster>` then `kubectl delete hcloudmachinetemplate <name>` then re-run
  - Post-bootstrap: use `kubeaid-cli cluster upgrade` instead
  - Finalizer hangs: `kubectl patch machine <name> --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'` ONLY when the cloud-side server is known not to exist

- **Wait stuck on `NodeHealthy: Waiting for Cluster control plane to be initialized`**
  - Phase=Provisioned + InfrastructureReady=true means the server is up but kubeadm hasn't completed on the node
  - VPN cluster has no public IPv4 on the control plane — SSH via the NAT gateway:
    ```bash
    ssh -J root@<nat-gateway-public-ip> root@<cp-private-ip>
    cloud-init status
    cat /var/log/cloud-init-output.log | tail -100
    journalctl -u kubelet -n 100 --no-pager
    ls /etc/kubernetes/    # admin.conf only present once kubeadm init finishes
    crictl ps -a           # are kube-apiserver / etcd static pods running?
    ```
  - Common failure modes: image pull stalled, etcd port bind failure, cert SAN mismatch

- **Manual NAT gateway deletion from HCloud console**
  - kubeaid-cli will recreate on re-run, but the deletion-protection toggle needs both `delete` and `rebuild` set together (per Hetzner API)
  - If kubeaid-cli predates the fix, manually toggle protection off before delete

### Sealed Secrets

- Sealed Secrets controller in management vs main cluster has different keys — re-encryption happens during `SetupCluster` on main cluster
- If a re-run hits "sealed secrets values mismatch", check the SHA-256 cache header `# kubeaid-sha256:` in each sealed-secret file under the kubeaid-config fork

### ArgoCD

- "Manifest generation error (cached)" — repo-server caches manifests; force a fresh comparison via:
  `kubectl annotate app <name> argocd.argoproj.io/refresh=hard --overwrite`

## Pending feature work

### Wire netbird-operator config (managementURL + API token)

`netBirdOperatorEnabled()` in `pkg/core/templates.go` now renders
the operator on both workload-with-Keycloak and VPN clusters (the
VPN-cluster install was the immediate fix that landed alongside
this TODO). But the values overlay
(`values-netbird-operator.yaml.tmpl`) is still empty — the operator
pod runs with chart defaults: no `managementURL`, no
`netbirdAPI.keyFromSecret`, no `cluster.name`. It can't talk to
NetBird Mgmt's API, so NBSetupKey / NBPolicy / NetworkRouter /
NetworkResource CRs created against it sit pending.

To make the operator actually usable, the v0.4.1 chart needs:

```yaml
managementURL: https://netbird.<parent-or-self-dns>
cluster:
  name: <cluster name>
  dns: svc.cluster.local
netbirdAPI:
  keyFromSecret:
    name: ...   # a SealedSecret kubeaid-cli renders
    key: ...
```

Open design decisions (deferred from a hands-on discussion — see the
chat that led to this TODO entry):

1. **Config shape in general.yaml.** Likely a new explicit
   `cluster.netbird: { mgmtURL, apiKeySecret: {name, key} }` block.
   Decoupling from `cluster.keycloak` matters for operators who
   bring their own NetBird without a parent VPN's Keycloak.

2. **NetBird API token source.** Three reasonable options:
   - Operator-provided, prompt for an existing Secret name/key.
     Mirrors the existing operator-managed Keycloak admin creds.
   - Auto-generated for managed-Keycloak VPN clusters: after
     NetBird Mgmt is healthy, kubeaid-cli signs in via a service
     user, mints a PAT through NetBird's API, stamps it into a
     SealedSecret. Needs a new `pkg/netbird/` Go client (similar
     shape to `pkg/keycloak/`).
   - Skip the API key on the first cut — render the operator pod
     without it, document the manual-wire-up. Smallest scope;
     CRDs and pod ship, operator finishes the wiring later.

3. **VPN cluster scope.** If we install the operator there with a
   self-referential `managementURL: https://netbird.<own-vpn-dns>`,
   it unlocks `NetworkRouter` / `NBRoutingPeer` CRs that close the
   "no peer in the HCloud private network advertises 10.0.0.0/24"
   gap from `docs/post-bootstrap.md`'s break-glass discussion —
   kubectl-over-mesh would work without ssh-jumping through NAT GW.

4. **Prompt UX.** Workload-cluster prompt currently derives the
   NetBird Mgmt URL by string-munging the Keycloak DNS
   (`keycloak.X` → `netbird.X` in `pkg/config/prompt/prompt.go:319`)
   instead of asking. Add an explicit "do you have a NetBird
   server?" group, similar in shape to `runWorkloadKeycloakForm`.

When picking this up: pair with the v0.4.1 chart bump
([kubeaid commit `7d6834816`](https://gitea.obmondo.com/Obmondo/kubeaid))
to get the new `NetworkRouter` / `NetworkResource` / `SidecarProfile`
CRDs while keeping the legacy `NB*` CRDs available for in-flight
configs.

### Hard-fail `kubeaid-cli login` on a NetBird mesh mismatch

`pickCluster` (`cmd/kubeaid-cli/login/login.go`) compares the local
NetBird daemon's management URL against `global.NetBird.ManagementURL`
from klist's `global.yaml`, but a mismatch only emits a `slog.Warn` —
login then proceeds against whatever mesh the daemon is on, leaving
the user with an easy-to-miss warning and a wrong or empty cluster
list.

The `bootstrap` pre-flight (`requireOperatorOnNetBird`) was made to
hard-fail on exactly this mismatch — see `fix(netbird): verify the
bootstrap operator is on the right mesh`. `login` should get the same
treatment: turn the warn-only branch in `pickCluster` into a returned
error that tells the user to `netbird up --management-url` against the
right server first.

Deferred rather than bundled into the bootstrap fix because `login`
keys off klist's `global.yaml`, not `general.yaml` — a different
config surface worth handling on its own.
