# Local K3D Demo Without a YubiKey

This guide bootstraps a local K3D cluster with KubeAid CLI using two ordinary
SSH keys instead of an SSH agent or a hardware key. It uses GitHub's web UI for
the one-time repository setup, then uses the CLI for the cluster.

The CLI uses Git in two different places:

1. **Operator key:** used on your machine while the CLI clones and pushes the
   generated configuration to your GitHub fork.
2. **Deploy key:** used by Argo CD inside the cluster after bootstrap, when it
   reads that fork.

Keep the keys separate. The operator key belongs to your GitHub account. The
deploy key belongs only to the one repository and is read-only.

## Prerequisites

- Docker is installed and running.
- The CLI is built from this checkout:

  ```sh
  make build
  ```

## Create the fork in GitHub

1. Open [Obmondo/kubeaid-config](https://github.com/Obmondo/kubeaid-config).
2. Click **Fork**.
3. Choose your account, keep the repository name `kubeaid-config`, and create
   the fork.

There is no need to clone the fork manually. At bootstrap the CLI clones your
fork into its workspace, writes the generated files, pushes a branch, and asks
you to merge that branch. It also clones the pinned KubeAid platform repository
it needs to render the configuration.

## Create the two keys

```sh
ssh-keygen -t ed25519 \
  -f ~/.ssh/kubeaid-demo-operator \
  -C "kubeaid-local-demo-operator" \
  -N ""

ssh-keygen -t ed25519 \
  -f ~/.ssh/kubeaid-demo-argocd \
  -C "kubeaid-local-demo-argocd" \
  -N ""
```

The first command creates `kubeaid-demo-operator` and
`kubeaid-demo-operator.pub`. The private key stays on your machine; the public
key is added to your GitHub account.

The second command creates `kubeaid-demo-argocd` and
`kubeaid-demo-argocd.pub`. Its private key stays on your machine until the CLI
seals it into the cluster. Its public key belongs only on the config fork.

In GitHub:

1. Open **your profile → Settings → SSH and GPG keys → New SSH key**. Paste
   the contents of `~/.ssh/kubeaid-demo-operator.pub`. This lets the CLI push
   to repositories owned by your account.
2. Open **your `kubeaid-config` fork → Settings → Deploy keys → Add deploy
   key**. Paste the contents of `~/.ssh/kubeaid-demo-argocd.pub`. Leave
   **Allow write access** unchecked. This lets Argo CD read the fork and
   nothing more.

Do not upload either private-key file or use the operator key as the deploy
key.

## Generate configuration

Use an explicit directory so the demo's configuration, kubeconfig, logs, and
K3D configuration stay together.

```sh
export DEMO_DIR="$HOME/kubeaid-local-demo"
mkdir -p "$DEMO_DIR"

./build/kubeaid-cli config generate \
  --configs-directory "$DEMO_DIR"
```

The first line names one directory for all demo files. The second creates it.
The CLI command writes `general.yaml` and `secrets.yaml` there.

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
them before proceeding. `secrets.yaml` contains sensitive values and must not
be committed or shared.

## Bootstrap and inspect

```sh
./build/kubeaid-cli cluster bootstrap \
  --configs-directory "$DEMO_DIR"
```

The CLI clones your config fork, creates the local K3D cluster, installs
Argo CD, renders the configuration into the clone, and pushes a branch. Merge
the printed pull request URL when asked, then let bootstrap continue.

```sh
export KUBECONFIG="$DEMO_DIR/kubeconfigs/management/host.yaml"
kubectl get nodes -o wide
kubectl get pods -A
kubectl get applications -n argocd
```

`KUBECONFIG` points `kubectl` at the local cluster. The three commands check
nodes, all workloads, and Argo CD applications.

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
