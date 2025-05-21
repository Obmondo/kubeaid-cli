# Setting up KubeAid on Hcloud

## Prerequisites

- First of all login to Hcloud
- Go to the 'hcloud' console by clicking on the grid icon.
- Then create a project and use this doc to generate API token - https://docs.hetzner.com/cloud/api/getting-started/generating-api-token
- Then generate a SSH key and add it in the 'SSH keys' section of security in your project by following the docs - https://docs.hetzner.com/cloud/servers/getting-started/connecting-to-the-server/#cli-warning .
- Now create a sample config similar to the one given below:

```
forkURLs:
  kubeaid: https://github.com/Archisman-Mridha/kubeaid
  kubeaidConfig: https://github.com/Archisman-Mridha/kubeaid-config

cluster:
  name: kubeaid-demo-image2work
  k8sVersion: v1.31.0
  kubeaidVersion: HEAD
  enableAuditLogging: true
  additionalUsers:
    - name: ***
      sshPublicKey: ***

cloud:
  hetzner:
    mode: hcloud

    zone: eu-central
    region: hel1

    hcloudSSHKeyPairName: demo01

    networkEnabled: true

    imageName: ubuntu-24.04

    controlPlane:
      machineType: cax11
      replicas: 3
      regions:
        - fsn1
        - nbg1
        - hel1
      loadBalancer:
        enabled: true
        region: hel1

    nodeGroups:
      hcloud:
        - name: bootstrapper
          machineType: cax11
          minSize: 1
          maxSize: 3
          labels:
            node-role.kubernetes.io/bootstrapper: ""
            node.cluster.x-k8s.io/nodegroup: bootstrapper
          taints: []
```

Create 