# Setting up a KubeAid managed cluster on Hetzner

## Hetzner installation requirement

* Hcloud(./hetzner/hcloud.md)
* Robot(./hetzner/robot.md)
* Hybrid(./hetzner/hybrid.md)

## Installation

### Method 1

* Download the compose file
```sh
wget https://raw.githubusercontent.com/Obmondo/kubeaid-bootstrap-script/refs/heads/main/docker-compose.yaml
```

* Add the cloud provider flavour

```
cat .env
CLOUD_PROVIDER=local
```

* Generate the config

```sh
docker compose run bootstrap-generate
```

* Add the user and ssh key in the general.yaml

```yaml
# Any additional users you want to be setup for each Kubernetes node.
# additionalUsers:
#  - name: archi
#    sshPublicKey: xxxxxxxxxx
```

* Add the git username and token in the secret.yaml

```yaml
git:
  username: xxxxxxxxxx
  password: xxxxxxxxxx
```

* Bootstrap the cluster

```sh
docker compose run bootstrap-cluster
```

* Get your KUBECONFIG

```sh
cat ./outputs/kubeconfigs/main.yaml
export KUBECONFIG=./outputs/kubeconfigs/main.yaml
```

## Destroy
When you want to destroy the cluster

```sh
docker compose down
k3d cluster delete management-cluster
```
