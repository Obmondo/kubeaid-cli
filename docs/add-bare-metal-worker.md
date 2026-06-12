# Adding a bare-metal worker node (via kubeaid-cli)

> The recommended way to grow a Hetzner bare-metal cluster's worker pool.
> For the emergency / no-CLI path, see
> [`add-bare-metal-worker-manual.md`](./add-bare-metal-worker-manual.md).
> Background on how a node gets provisioned end-to-end:
> [`bare-metal-provisioning.md`](./bare-metal-provisioning.md).

Adding a worker is purely additive: the chart renders one
`HetznerBareMetalHost` + `HetznerBareMetalMachine` + `Machine` +
`KubeadmConfig` per `bareMetalHosts[]` entry, so a new entry provisions one
new node. Existing machines are untouched — no control-plane roll, no
worker restarts.

## Prerequisites (Hetzner side)

These live outside the cluster config and must be done first:

1. **Server exists in Hetzner Robot** — ordered, installed with rescue
   access, visible in your Robot account (note its numeric server ID).
2. **Attached to the cluster's vSwitch** — Robot UI → vSwitch → add the
   server. Without vSwitch membership the node gets no private path, no
   InternalIP, and apiserver→kubelet streaming (logs / exec /
   port-forward) breaks.
3. **A free private IP** from the vSwitch subnet
   (`cloud.hetzner.bareMetal.vSwitch.subnetCIDRBlock`) — must not collide
   with any existing host.
4. **The disk WWNs** — boot the server into rescue mode and run:

   ```bash
   lsblk -dno NAME,WWN
   ```

   NVMe disks show as `eui.…`, SATA/SAS as `0x…`. The storage planner
   uses these to build the vg0 / ZFS layout.

## Steps

1. Add the host to the node-group in your cluster's `general.yaml`:

   ```yaml
   cloud:
     hetzner:
       nodeGroups:
         bareMetal:
           - name: workers
             labels: {}
             taints: []
             zfs:
               size: 220
             bareMetalHosts:
               - serverID: "1414813"        # existing
                 privateIP: 10.0.1.4
                 wwns:
                   - "0x5000cca25ede270a"
                   - "0x5000cca25eccbe9f"
               - serverID: "1500000"        # the new server
                 privateIP: 10.0.1.6
                 wwns:
                   - "eui.002538b121b71e1e"
                   - "eui.002538b341beb77c"
   ```

   `general.yaml` is the source of truth — every rendered file in
   kubeaid-config derives from it.

2. Re-run the same bootstrap command you provisioned the cluster with:

   ```bash
   kubeaid-cli cluster bootstrap ...
   ```

   The run is idempotent. For an existing cluster it re-renders the
   kubeaid-config files, commits + pushes them, and syncs the ArgoCD
   apps. Two rendered files change for a new worker:

   - `argocd-apps/values-capi-cluster.yaml` — the new
     `bareMetalHosts[]` entry (per-host CAPI resources).
   - `argocd-apps/values-kubelet-csr-approver.yaml` — the approver's
     `providerIpPrefixes` gains the new server's public `/32`.
     kubeaid-cli queries the Robot API for every host's public IP at
     render time; this is why the CLI flow is preferred over hand-editing
     — miss this file and the new kubelet's serving-certificate CSR is
     denied, which surfaces as `Unauthorized` on logs / exec /
     port-forward against pods on that node.

3. CAPH takes over: rescue boot → install-image → cloud-init → `kubeadm
   join`. Watch it converge:

   ```bash
   kubectl get hetznerbaremetalhosts -A    # new host: provisioning → provisioned
   kubectl get machines -A                 # new Machine: Provisioning → Running
   ```

## Verify

```bash
# Node joined, Ready, and has the vSwitch IP as InternalIP:
kubectl get nodes -o wide

# Its kubelet serving CSR got approved (not Denied):
kubectl get csr | tail

# Pods on the new node resolve cluster Services (not the node's 1.1.1.1):
kubectl run dnstest --rm -it --restart=Never \
  --overrides='{"spec":{"nodeName":"<new-node-name>"}}' \
  --image=busybox -- cat /etc/resolv.conf
#   -> nameserver 10.96.0.10

# The streaming path works (exercises the serving cert):
kubectl logs -n kube-system --tail=1 <any-pod-on-the-new-node>
```
