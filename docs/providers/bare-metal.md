# Bare Metal Provider

The `bare-metal` provider is used to provision a KubeAid managed Kubernetes cluster across a given set of bare metal Linux machines, which has the following setup :

- [Cilium](https://cilium.io) CNI, running in [kube-proxyless mode](https://cilium.io/use-cases/kube-proxy/).

- [Azure Workload Identity](https://azure.github.io/azure-workload-identity/docs/).

- Node-groups, with **labels and taints propagation** support.

- GitOps, using [ArgoCD](https://argoproj.github.io/cd/) and [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets).

- Monitoring, using [KubePrometheus](https://prometheus-operator.dev).

> Since the lifecycle of those Linux machines need not to be managed, we use [Kubermatic KubeOne](https://github.com/kubermatic/kubeone) under the hood to initialize the Kubernetes cluster.

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

- Create a `.env` file, in your working directory, with the following content :
  ```env
  CLOUD_PROVIDER=bare-metal
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
