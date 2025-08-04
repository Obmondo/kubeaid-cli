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

- Create a Hetzner Bare Metal SSH KeyPair, by visiting <https://robot.hetzner.com/key/index>.
  > Ensure that you don't already have a Hetzner Bare Metal SSH KeyPair with the SSH key-pair
  > you'll be using.
  > Otherwise, ClusterAPI Provider Hetzner (CAPH) will error out.

- If you're going to use RAID, then remove any pre-existing RAID setup from the Hetzner Bare Metal servers.

  You can do so, by executing the following in each Hetzner Bare Metal server :
  ```shell script
  wipefs -fa /dev/sda
  wipefs -fa /dev/sdb
  ```

## Choose your UX

KubeAid Bootstrap Script depends on the following CLI tools during runtime :

- [jsonnet](https://github.com/google/jsonnet?tab=readme-ov-file#packages), [jsonnet-bundler](https://github.com/jsonnet-bundler/jsonnet-bundler?tab=readme-ov-file#package-install) and [gojsontoyaml](https://github.com/brancz/gojsontoyaml?tab=readme-ov-file#install)

- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

You can either :

- First, install them on your host system.
  We provide a convenience [Bash script](https://github.com/Obmondo/kubeaid-bootstrap-script/blob/main/scripts/install-runtime-dependencies.sh), which you can use like so to get them :
  ```shell script
  CLOUD_PROVIDER=hetzner
  wget -qO - https://raw.githubusercontent.com/Obmondo/kubeaid-bootstrap-script/refs/heads/main/scripts/install-runtime-dependencies.sh | sh
  ```

  Then, grab the KubeAid Bootstrap Script binary, from our [releases page](https://github.com/Obmondo/kubeaid-bootstrap-script/releases) :
  ```shell scrip
  KUBEAID_BOOTSTRAP_SCRIPT_VERSION=$(curl -s "https://api.github.com/repos/Obmondo/kubeaid-bootstrap-script/releases/latest" | jq -r .tag_name)

  OS=$([ "$(uname -s)" = "Linux" ] && echo "linux" || echo "darwin")
  CPU_ARCHITECTURE=$([ "$(uname -m)" = "x86_64" ] && echo "amd64" || echo "arm64")

  curl -L -o kubeaid-bootstrap-script "https://github.com/Obmondo/kubeaid-bootstrap-script/releases/download/${KUBEAID_BOOTSTRAP_SCRIPT_VERSION}/kubeaid-bootstrap-script-${OS}-${CPU_ARCHITECTURE}-${KUBEAID_BOOTSTRAP_SCRIPT_VERSION}-${OS}-${CPU_ARCHITECTURE}"

  mv kubeaid-bootstrap-script /usr/local/bin
  ```

  And run it directly on your host system.

Or rather, use the KubeAid Bootstrap Script container image, which contains all the required runtime dependencies bundled in it, like so :

```shell script
KUBEAID_BOOTSTRAP_SCRIPT_VERSION=$(curl -s "https://api.github.com/repos/Obmondo/kubeaid-bootstrap-script/releases/latest" | jq -r .tag_name)

MANAGEMENT_CLUSTER_NAME="kubeaid-bootstrapper"

CONTAINER_IMAGE_NAME="ghcr.io/obmondo/kubeaid-bootstrap-script:${KUBEAID_BOOTSTRAP_SCRIPT_VERSION}"
CONTAINER_NETWORK_NAME="k3d-${MANAGEMENT_CLUSTER_NAME}"
CONTAINER_NAME="kubeaid-bootstrap-script"

cat <<EOF > kubeaid-bootstrap-script.sh
  if ! docker network ls | grep -q "${NETWORK_NAME}"; then \
    docker network create "${NETWORK_NAME}"; \
  fi

  docker run --name "${CONTAINER_NAME}" \\
    --network "${CONTAINER_NETWORK_NAME}" \\
    -v ./outputs:/outputs \\
    -v /var/run/docker.sock:/var/run/docker.sock \\
    --rm \\
    "${CONTAINER_IMAGE_NAME}" "\$@"
EOF

chmod +x kubeaid-bootstrap-script.sh

alias kubeaid-bootstrap-script="$(pwd)/kubeaid-bootstrap-script.sh"
```

## Preparing the Configuration Files

You need to have 2 configuration files : `general.yaml` and `secrets.yaml` containing required credentials.

Run :
```shell script
kubeaid-bootstrap-script config generate hetzner bare-metal
```
and a sample of those 2 configuration files will be generated in `outputs/configs`.

Edit those 2 configuration files, based on your requirements.

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

## Deleting the Cluster

You can delete the cluster, by running :
```shell script
kubeaid-bootstrap-script cluster delete main
kubeaid-bootstrap-script cluster delete management
```
