# Hetzner KubeAid managed cluster

## Installation

* Download the compose file
```sh
wget https://raw.githubusercontent.com/Obmondo/kubeaid-bootstrap-script/refs/heads/main/docker-compose.yaml
```

* Add the cloud provider and flavour

```
# cat .env
CLOUD_PROVIDER=hetzner
FLAVOR=hcloud
CLUSTER_NAME=kubeaid-demo
```

* Generate the config, which will be created under `./outputs/configs`

```sh
docker compose run bootstrap-generate
```

## Choose your flavor

* [Hcloud](./hetzner/hcloud.md)
* [Robot](./hetzner/robot.md)
* [Hybrid](./hetzner/hybrid.md)

## Access your cluster

* Get your KUBECONFIG

```sh
cat ./outputs/kubeconfigs/main.yaml
export KUBECONFIG=./outputs/kubeconfigs/main.yaml
```

## Destroy

* When you want to destroy the cluster

```sh
k3d cluster delete kubeaid-demo
```
