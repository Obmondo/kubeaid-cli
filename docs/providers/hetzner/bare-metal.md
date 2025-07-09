# Hetzner Provider : Bare Metal mode

The `hetzner` provider, in `bare-metal` mode, is used to provision a KubeAid managed Kubernetes cluster in Hetzner Bare-Metal, which has the following setup :

- [Cilium](https://cilium.io) CNI, running in [kube-proxyless mode](https://cilium.io/use-cases/kube-proxy/).

- Node-groups, with **labels and taints propagation** support.

- GitOps, using [ArgoCD](https://argoproj.github.io/cd/), [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) and [ClusterAPI](https://cluster-api.sigs.k8s.io).

- Monitoring, using [KubePrometheus](https://prometheus-operator.dev).

## Prerequisites

- Fork the [KubeAid Config](https://github.com/Obmondo/kubeaid-config) repository.

- Keep your Git provider credentials ready.
  > For GitHub, you can create a [Personal Access Token (PAT)](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token), which has the permission to write to your KubeAid Config fork.
  > That PAT will be used as the password.

- Have [Docker](https://www.docker.com/products/docker-desktop/) running locally.

- Get the utility [docker-compose](https://github.com/Obmondo/kubeaid-bootstrap-script/blob/main/docker-compose.yaml) file, by running :
  ```shell script
  wget https://raw.githubusercontent.com/Obmondo/kubeaid-bootstrap-script/refs/heads/main/docker-compose.yaml
  ```

- Create a Hetzner Bare Metal SSH KeyPair, by visiting <https://robot.hetzner.com/key/index>.

- If you're going to use RAID, then remove any pre-existing RAID setup from the Hetzner Bare Metal servers.

  You can do so, by executing the following in each Hetzner Bare Metal server :
  ```shell script
  wipefs -fa /dev/sda
  wipefs -fa /dev/sdb
  ```

## Preparing the Configuration Files

You need to have 2 configuration files : `general.yaml` and `secrets.yaml` containing required credentials.

Run :
```shell script
docker compose run config-generate
```
and a sample of those 2 configuration files will be generated in `outputs/configs`.

Edit those 2 configuration files, based on your requirements.

## Bootstrapping the Cluster

Run the following command, to bootstrap the cluster :
```shell script
docker compose run bootstrap-cluster
```

Aside from the logs getting streamed to your standard output, they'll be saved in `outputs/.log`.

Once the cluster gets bootstrapped, its kubeconfig gets saved in `outputs/kubeconfigs/clusters/main.yaml`.

You can access the cluster, by running :
```shell script
export KUBECONFIG=./outputs/kubeconfigs/main.yaml
kubectl cluster-info
```
Go ahead and explore it by accessing the [ArgoCD]() and [Grafana]() dashboards.

## Upgrading the Cluster

In `outputs/configs/general.yaml`, change the `cluster.k8sVersion` to the Kubernetes version you want to upgrade to.

Then re-run :
```shell script
docker compose run bootstrap-cluster
```

## Deleting the Cluster

You can delete the cluster, by running :
```shell script
docker compose run delete-cluster
docker compose run cleanup
```
