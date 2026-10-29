# Local K3D Demo Without a YubiKey

This guide bootstraps a local K3D cluster with KubeAid CLI using two ordinary
SSH keys instead of an SSH agent or a hardware key.

The CLI needs Git access for two different jobs:

1. The operator key clones and pushes the generated configuration to your
   GitHub fork during bootstrap.
2. The deploy key lets Argo CD inside the cluster read that fork after
   bootstrap.

Keep the keys separate. The operator key belongs to your GitHub account. The
deploy key belongs only to the one repository and is read-only.

## Prerequisites

- Docker is installed and running.
- GitHub CLI is authenticated with an account that can create a fork:

  ```sh
  gh auth login
  ```

- The CLI is built from this checkout:

  ```sh
  make build
  ```

## Create the fork and keys

Replace `YOUR_GITHUB_USER` with your GitHub user name.

```sh
gh repo fork Obmondo/kubeaid-config \
  --fork-name kubeaid-config \
  --clone=false

ssh-keygen -t ed25519 \
  -f ~/.ssh/kubeaid-demo-operator \
  -C "kubeaid-local-demo-operator" \
  -N ""

ssh-keygen -t ed25519 \
  -f ~/.ssh/kubeaid-demo-argocd \
  -C "kubeaid-local-demo-argocd" \
  -N ""

gh ssh-key add ~/.ssh/kubeaid-demo-operator.pub \
  --title "kubeaid-local-demo-operator"

gh repo deploy-key add ~/.ssh/kubeaid-demo-argocd.pub \
  --repo YOUR_GITHUB_USER/kubeaid-config \
  --title "kubeaid-local-demo-argocd"
```

`gh ssh-key add` authorizes the operator key to push to repositories owned by
your account. `gh repo deploy-key add` gives the second key read-only access to
the fork. Do not reuse the operator key as the deploy key.

## Generate configuration

Use an explicit directory so the demo's configuration, kubeconfig, logs, and
K3D configuration stay together.

```sh
export DEMO_DIR="$HOME/kubeaid-local-demo"
mkdir -p "$DEMO_DIR"

./build/kubeaid-cli config generate \
  --configs-directory "$DEMO_DIR"
```

In the interactive prompt:

- Choose the **Local / K3D** provider.
- Choose **No** when asked to use the SSH agent.
- Set the operator private key to:

  ```text
  ~/.ssh/kubeaid-demo-operator
  ```

- Set the Argo CD deploy-key private key to:

  ```text
  ~/.ssh/kubeaid-demo-argocd
  ```

- Set the KubeAid Config fork URL to:

  ```text
  git@github.com:YOUR_GITHUB_USER/kubeaid-config.git
  ```

The prompt writes `general.yaml` and `secrets.yaml` to `$DEMO_DIR`. Review
them before proceeding.

## Bootstrap and inspect

```sh
./build/kubeaid-cli cluster bootstrap \
  --configs-directory "$DEMO_DIR"

export KUBECONFIG="$DEMO_DIR/kubeconfigs/management/host.yaml"

kubectl get nodes -o wide
kubectl get pods -A
kubectl get applications -n argocd
```

For a local K3D provider, the K3D cluster is both the management cluster and
the workload cluster, which is why the kubeconfig is under
`kubeconfigs/management/host.yaml`.

## Clean up

After the demo, remove the local K3D management cluster:

```sh
./build/kubeaid-cli cluster delete management \
  --configs-directory "$DEMO_DIR"
```

Keep `$DEMO_DIR` until you no longer need the bootstrap logs, generated
configuration, or kubeconfig.
