# Azure Provider

The `azure` provider is used to provision a KubeAid managed Kubernetes cluster in Azure, which has the following setup :

- [Cilium](https://cilium.io) CNI, running in [kube-proxyless mode](https://cilium.io/use-cases/kube-proxy/).

- [Azure Workload Identity](https://azure.github.io/azure-workload-identity/docs/).

- Autoscalable node-groups, with **scale to / from 0** and **labels and taints propagation** support.

- GitOps, using [ArgoCD](https://argoproj.github.io/cd/), [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets), [ClusterAPI](https://cluster-api.sigs.k8s.io) and [CrossPlane](https://www.crossplane.io).

- Monitoring, using [KubePrometheus](https://prometheus-operator.dev).

- Disaster Recovery, using [Velero](https://velero.io).

## Prerequisites

- Fork the [KubeAid Config](https://github.com/Obmondo/kubeaid-config) repository.

- Keep your Git provider credentials ready.
  > For GitHub, you can create a [Personal Access Token (PAT)](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token), which has the permission to write to your KubeAid Config fork.
  > That PAT will be used as the password.

- A Linux or MacOS computer, with atleast 16GB of RAM.
  > You can try with one having 8GB RAM, but some of the pods might get killed due to OOM issue.

- Have [Docker](https://www.docker.com/products/docker-desktop/) running locally.

- [Register an application (Service Principal) in Microsoft Entra ID](https://learn.microsoft.com/en-us/entra/identity-platform/quickstart-register-app).

- An OpenSSH type SSH keypair, whose private key you'll use to SSH into the VMs.

- A PEM type SSH keypair, which will be used for Azure Workload Identity setup.

Additionally, have the following runtime dependencies installed :

- [jsonnet](https://github.com/google/jsonnet?tab=readme-ov-file#packages), [jsonnet-bundler](https://github.com/jsonnet-bundler/jsonnet-bundler?tab=readme-ov-file#package-install) and [gojsontoyaml](https://github.com/brancz/gojsontoyaml?tab=readme-ov-file#install)

- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

- [kubeseal](https://github.com/bitnami-labs/sealed-secrets?tab=readme-ov-file#installation)

- [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start#install-clusterctl)

- [k3d](https://k3d.io/stable/#installation)

## Preparing the Configuration Files

You need to have 2 configuration files : `general.yaml` and `secrets.yaml` containing required credentials.

Run :
```shell script
kubeaid-bootstrap-script config generate azure
```
and a sample of those 2 configuration files will be generated in `outputs/configs`.

Edit those 2 configuration files, based on your requirements.
> Let's assuming that, you'll be using Kubernetes `v1.31.0`.

## Bootstrapping the Cluster

Run the following command, to bootstrap the cluster :
```shell script
kubeaid-bootstrap-script cluster bootstrap
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

Let's upgrade the cluster from Kubernetes `v1.31.0` to `v1.32.0` :
```shell script
kubeaid-bootstrap-script cluster upgrade \
  --new-k8s-version v1.32.0
```

> If you want to do an OS upgrade as well, you can specify the new Canonical Ubuntu image offer to be used, via the `--new-image-offer` flag.

## Deleting the Cluster

You can delete the cluster, by running :
```shell script
kubeaid-bootstrap-script cluster delete
k3d cluster delete kubeaid-bootstrapper
```
