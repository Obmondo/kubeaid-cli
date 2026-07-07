# Troubleshooting

Recovery paths for recurring `kubeaid-cli` bootstrap failures, so you don't
have to re-discover them from chat logs. Grouped by where the failure surfaces.

## Hetzner

### `CAX` server type not available in the datacenter

Bootstrap fails with `error during placement (resource_unavailable, ...)`.

- **Transient** — Hetzner is temporarily out of capacity for that type in that
  location. Retry, or pick a different location/type.
- **Misconfiguration** — the requested type isn't offered in that location
  (e.g. an Arm `CAX` type in an x86-only datacenter). It will never succeed as-is.

Fix: set a valid type in `Cloud.Hetzner.ControlPlane.HCloud.MachineType` (and the
node-pool equivalents) and re-run.

### `HCloudMachineTemplate.Spec is immutable` on ArgoCD sync after a config change

CAPH treats machine-template specs as immutable, so editing a template in place is
rejected on sync.

- **Post-bootstrap** — don't hand-edit the template; use `kubeaid-cli cluster upgrade`,
  which rolls a new one.
- **Pre-bootstrap recovery** (the cluster isn't live yet) — delete the machines and the
  template, then re-run:

  ```bash
  kubectl delete machine -l cluster.x-k8s.io/cluster-name=<cluster>
  kubectl delete hcloudmachinetemplate <name>
  ```

- **Finalizer hangs** — only when you know the cloud-side server no longer exists:

  ```bash
  kubectl patch machine <name> --type=json -p='[{"op":"remove","path":"/metadata/finalizers"}]'
  ```

### Stuck at `NodeHealthy: Waiting for Cluster control plane to be initialized`

`Phase=Provisioned` + `InfrastructureReady=true` means the server is up but kubeadm
hasn't finished on the node.

A VPN cluster has no public IPv4 on the control plane, so SSH in via the NAT gateway
and inspect:

```bash
ssh -J root@<nat-gateway-public-ip> root@<cp-private-ip>
cloud-init status
tail -100 /var/log/cloud-init-output.log
journalctl -u kubelet -n 100 --no-pager
ls /etc/kubernetes/    # admin.conf appears only once kubeadm init finishes
crictl ps -a           # are the kube-apiserver / etcd static pods running?
```

Common causes: a stalled image pull, an etcd port-bind failure, or a cert SAN mismatch.

### NAT gateway deleted manually from the HCloud console

`kubeaid-cli` recreates it on the next run. Note that Hetzner's deletion-protection
toggle must set `delete` and `rebuild` together — if your `kubeaid-cli` predates that
fix, toggle protection off manually before deleting.

## Sealed Secrets

- The Sealed Secrets controller has **different keys on the management vs the main
  cluster**; re-encryption happens during `SetupCluster` on the main cluster.
- If a re-run hits `sealed secrets values mismatch`, check the `# kubeaid-sha256:`
  cache header in each sealed-secret file under the kubeaid-config fork.

## ArgoCD

- **`Manifest generation error (cached)`** — the repo-server caches manifests; force a
  fresh comparison:

  ```bash
  kubectl annotate app <name> argocd.argoproj.io/refresh=hard --overwrite
  ```
