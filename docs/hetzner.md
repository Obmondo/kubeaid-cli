# Setting up a KubeAid managed cluster on HCloud

Make sure you to follow the pre-requisite for each flavor first given in docs/hetzner folder before proceeding. KubeAid Bootstrap Script requires quite a few prerequisite tools (like gojsontoyaml, kubeseal etc.) installed in your system. This is why, we've published a [container image](https://github.com/Obmondo/kubeaid-bootstrap-script/pkgs/container/kubeaid-bootstrap-script) which has all those prerequisites tools along with the `kubeaid-bootstrap-script` binary packed in it. Let's pull that container image :

```sh
docker pull ghcr.io/obmondo/kubeaid-bootstrap-script:v0.11.1
```

Run and exec into the KubeAid Bootstrap Script container :

```sh
NETWORK_NAME="k3d-management-cluster"
if ! docker network ls | grep -q "$NETWORK_NAME"; then
    docker network create "$NETWORK_NAME"
fi

docker run --name kubeaid-bootstrap-script \
  --network "$NETWORK_NAME" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v ./outputs:/outputs \
  -d \
  ghcr.io/obmondo/kubeaid-bootstrap-script:v0.11.1

docker exec -it kubeaid-bootstrap-script /bin/bash
```

Generate sample `general` and `secret` config files in `./outputs/configs`, by running :

```sh
kubeaid-bootstrap-script config generate hetzner <flavor>
```

Use 'robot' as the <flavor> to work with bare-metal hetzner servers and robot-api. Use 'cloud' as the flavor to work with hcloud servers. Support for 'hybrid' flavor i.e. to use control-plane of type 'hcloud' or 'bare-metal' servers and worker nodes of different type is coming soon.

Each field in those 2 YAML files are documented. Go through them and replace dummy values accordingly.

Once done, run :

```sh
kubeaid-bootstrap-script cluster bootstrap
```

Please note that once the script starts running, you will need to manually merge the PR on your Git hosting platform (e.g., GitHub) whenever the logs display instructions to do so.

Once the main cluster has been provisioned, you can find it's kubeconfig at `./outputs/kubeconfigs/main.yaml`.

When you want to destroy the cluster, just do :

```sh
kubeaid-bootstrap-script cluster delete
k3d cluster delete management-cluster
```
