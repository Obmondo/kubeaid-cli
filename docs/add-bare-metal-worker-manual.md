# Adding a bare-metal worker node (manually, git-only)

> The break-glass path: grow the worker pool by editing the rendered
> files in kubeaid-config directly, without running kubeaid-cli. Use
> [`add-bare-metal-worker.md`](./add-bare-metal-worker.md) (the CLI flow)
> whenever you can — it renders every affected file for you and pulls the
> server's public IP from the Robot API.

The chart renders one `HetznerBareMetalHost` + `HetznerBareMetalMachine` +
`Machine` + `KubeadmConfig` per `bareMetalHosts[]` entry, so adding an
entry and syncing provisions one new node. Existing machines are
untouched.

## Prerequisites (Hetzner side)

Same as the CLI flow — see
[`add-bare-metal-worker.md`](./add-bare-metal-worker.md#prerequisites-hetzner-side):
server in Robot, attached to the cluster's vSwitch, a free private IP
from the vSwitch subnet, and the disk WWNs from rescue mode
(`lsblk -dno NAME,WWN`).

You additionally need the server's **public IP** (Robot UI, or):

```bash
curl -su "$ROBOT_USER:$ROBOT_PASSWORD" \
  https://robot-ws.your-server.de/server/<server-id> | jq -r .server.server_ip
```

## Steps

All edits happen in your kubeaid-config clone, under
`<customer>/k8s/<cluster>/argocd-apps/`.

1. **`values-capi-cluster.yaml`** — add the host to the node-group:

   ```yaml
   hetzner:
     nodeGroups:
       bareMetal:
         - name: workers
           bareMetalHosts:
             # ... existing hosts ...
             - serverID: "1500000"
               privateIP: 10.0.1.6
               wwns:
                 - "eui.002538b121b71e1e"
                 - "eui.002538b341beb77c"
   ```

2. **`values-kubelet-csr-approver.yaml`** — append the new server's
   public `/32` to `providerIpPrefixes` (comma-separated):

   ```yaml
   kubelet-csr-approver:
     providerIpPrefixes: "10.0.1.0/24,198.51.100.10/32,198.51.100.11/32,<new-public-ip>/32"
   ```

   Keep it single-line and comma-separated with **no whitespace** — the
   approver splits on `,` and feeds each entry to `netip.ParsePrefix`
   without trimming; a stray space crashes the controller on startup.

   This is the step the CLI automates and the one most easily missed:
   the kubelet's serving certificate carries both the vSwitch IP and the
   public IP as SANs, and the approver denies any CSR whose SANs aren't
   covered. A denied CSR doesn't block the node from joining — it
   surfaces later as `Unauthorized` on logs / exec / port-forward
   against pods on that node.

3. **Mirror the host into `general.yaml`** (the kubeaid-cli cluster
   config) even though nothing reads it right now.

   **This is not optional.** The rendered files are derived state: the
   next `kubeaid-cli cluster bootstrap` / upgrade re-renders them from
   `general.yaml` and pushes the result. If the host only exists in git,
   that re-render removes it — ArgoCD then deletes the node's `Machine`
   and CAPH **deprovisions the live worker**.

4. Commit and push.

5. Sync the two ArgoCD apps, **approver first** so the allow-list is
   live before the new kubelet submits its first CSR:

   ```bash
   argocd app sync kubelet-csr-approver
   argocd app sync capi-cluster
   ```

   (Or sync them from the UI / k9s in the same order.)

6. CAPH provisions the node: rescue boot → install-image → cloud-init →
   `kubeadm join`. Watch:

   ```bash
   kubectl get hetznerbaremetalhosts -A
   kubectl get machines -A
   ```

## Verify

Identical to the CLI flow — see
[`add-bare-metal-worker.md`](./add-bare-metal-worker.md#verify):
node Ready with InternalIP, CSR approved, pod `/etc/resolv.conf` pointing
at `10.96.0.10`, and `kubectl logs` working against a pod on the node.

## Caveat

Until the host is in `general.yaml`, every kubeaid-cli re-render is a
loaded gun pointed at this node (step 3). Do the mirror edit in the same
change, not "later".
