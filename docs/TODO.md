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
