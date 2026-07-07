# KubeAid CLI

[![Release](https://github.com/Obmondo/kubeaid-cli/actions/workflows/release.yaml/badge.svg)](https://github.com/Obmondo/kubeaid-cli/actions/workflows/release.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Obmondo/kubeaid-cli)](https://goreportcard.com/report/github.com/Obmondo/kubeaid-cli)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

KubeAid CLI operates the full lifecycle of [KubeAid](https://github.com/Obmondo/KubeAid)-managed Kubernetes clusters — bootstrap, upgrade, recover, test, and delete — across AWS, Azure, Hetzner, and bare metal, the GitOps-native way.

## Table of contents

- [Architecture](#architecture)
- [Features](#features)
- [Installation](#installation)
- [Prerequisites](#prerequisites)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Cloud providers](#cloud-providers)
- [Kubernetes version support](#kubernetes-version-support)
- [Configuration](#configuration)
- [Documentation](#documentation)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Architecture

KubeAid CLI is a **single self-contained binary**. The only local requirement is **Docker**, used to run a local [K3D](https://k3d.io/) cluster.

How it provisions depends on the target:

- **Cluster API clouds** — **AWS** (CAPA), **Azure** (CAPZ + Crossplane), and **Hetzner** (CAPH): it stands up a throwaway **K3D management cluster**, installs Cluster API there, provisions your target cluster, then `clusterctl move` **pivots** every Cluster API resource onto the target so it self-manages and the K3D cluster is discarded.
- **Generic bare metal** — **KubeOne** installs Kubernetes straight onto your hosts, with no K3D or Cluster API.
- **Local** — the K3D cluster is simply the cluster itself.

From there it is **GitOps**. The engine renders your `general.yaml` into manifests and commits them to your own **KubeAid Config** repo that overrides only the genuine differences on top of the upstream [KubeAid](https://github.com/Obmondo/KubeAid) platform defaults; [ArgoCD](https://argo-cd.readthedocs.io/) on the target then reconciles the addon stack — Cilium, cert-manager, kube-prometheus, Rook-Ceph, Velero, Sealed Secrets, and more. For the full breakdown, see [`docs/architecture.md`](docs/architecture.md).

## Features

- **Cluster lifecycle management** — bootstrap, upgrade, recover, test, and delete Kubernetes clusters
- **Development environments** — spin up local K3D-based dev clusters
- **Multi-cloud support** — AWS, Azure, Hetzner (cloud, bare-metal, hybrid), and generic bare-metal
- **GitOps native** — integrates with ArgoCD, KubeAid Config repos, and sealed secrets
- **Config generation** — generate sample configuration files per cloud provider

## Installation

### Shell script (Linux / macOS)

```sh
curl -fsSL https://raw.githubusercontent.com/Obmondo/kubeaid-cli/main/scripts/install.sh | sh
```

Supports `x86_64` and `arm64` on Linux and macOS. Installs to `/usr/local/bin` (may prompt for `sudo`).

### Nix

```sh
nix profile install github:Obmondo/kubeaid-cli
```

### Homebrew (macOS)

```sh
brew install Obmondo/kubeaid-cli/kubeaid-cli
```

### From source

```sh
go install github.com/Obmondo/kubeaid-cli/cmd/kubeaid-cli@latest
```

## Prerequisites

- **Docker** — must be installed and running (used to run the local K3D cluster)
- **SSH access to your Git repos** — either an `ssh-agent` with your key loaded, or an unencrypted private key file (`privateKeyFilePath` in `general.yaml`); use the agent for passphrased or YubiKey-backed keys

## Quick start

1. Walk through the interactive prompt to generate `general.yaml` and `secrets.yaml`:

   ```sh
   kubeaid-cli config generate --configs-directory ./outputs/configs/<cluster>/
   ```

2. Review the generated files (the prompt covers everything required to
   bootstrap; hand-edit only when you want to override defaults).

3. Bootstrap the cluster:

   ```sh
   kubeaid-cli cluster bootstrap --configs-directory ./outputs/configs/<cluster>/
   ```

   `cluster bootstrap` fails fast if the configs are missing — run
   `config generate` first.

## Usage

```
kubeaid-cli [command] [flags]
```

### Commands

| Command | Description |
|---|---|
| `config generate` | Interactively generate `general.yaml` and `secrets.yaml` via the config prompt |
| `devenv create` | Create a local development environment |
| `cluster bootstrap` | Bootstrap a new Kubernetes cluster |
| `cluster upgrade <provider>` | Upgrade an existing cluster |
| `cluster recover <provider>` | Recover a cluster |
| `cluster test` | Run tests against a cluster |
| `cluster delete` | Delete a provisioned cluster |
| `version` | Print version, commit, and build date |

### Global flags

| Flag | Description |
|---|---|
| `--debug` | Enable debug logging |
| `--configs-directory` | Path to directory containing `general.yaml` and `secrets.yaml` |

## Cloud providers

| Provider | Bootstrap | Upgrade | Recover | Delete |
|---|---|---|---|---|
| AWS | Yes | Yes | Yes | Yes |
| Azure | Yes | Yes | Yes | Yes |
| Hetzner Cloud | Yes | WIP | WIP | Yes |
| Hetzner Bare Metal | Yes | WIP | WIP | Yes |
| Hetzner Hybrid | Yes | WIP | WIP | Yes |
| Bare Metal | Yes | Yes | — | Yes |
| Local (K3D) | Yes | — | — | — |

`WIP` — work in progress; landing soon, not yet generally available.

## Kubernetes version support

The Kubernetes version you request is validated when the config is parsed: it must start with `v`, be a **released** version, be **within the supported range**, and **not past end-of-life**. End-of-life is checked against [endoflife.date](https://endoflife.date/kubernetes) data embedded in the binary (refresh it with `make fetch-k8s-eol`).

| KubeAid CLI | Kubernetes (AWS / Azure / Hetzner) | Kubernetes (bare metal · KubeOne) |
|---|---|---|
| `v0.29.x` | `v1.30` → latest released (non-EOL) | `v1.32` – `v1.34` |

- The **minimum** accepted version is `v1.30`; the **maximum** is the latest released minor. Any version past end-of-life is rejected regardless of the range.
- **Bare-metal** clusters are provisioned by **KubeOne v1.12**, which fixes the range to `v1.32`–`v1.34`; this moves when KubeOne is upgraded.
- The monitoring stack (KubePrometheus) is pinned per Kubernetes version via a built-in compatibility matrix covering `v1.32`–`v1.36`; note that `cgroup v1` support ends at `v1.35`.

This matrix is maintained per release — update the row whenever the supported range, KubeOne version, or a pinned component changes.

## Configuration

KubeAid CLI uses two YAML config files:

- **`general.yaml`** — cluster settings, cloud provider config, ArgoCD deploy keys, Git repo URLs, node groups, etc.
- **`secrets.yaml`** — cloud credentials, tokens, and other sensitive values.

See [`docs/config-reference.md`](docs/config-reference.md) for the full configuration reference.

## Documentation

Day-to-day operator guides:

- [`docs/post-bootstrap.md`](docs/post-bootstrap.md) — what to do right after a cluster comes up
- [`docs/add-bare-metal-worker.md`](docs/add-bare-metal-worker.md) — grow a Hetzner bare-metal worker pool (and the [manual git-only flow](docs/add-bare-metal-worker-manual.md))
- [`docs/upgrade-bare-metal.md`](docs/upgrade-bare-metal.md) — upgrade the Kubernetes version of a bare-metal (KubeOne) cluster

Identity and SSO:

- [`docs/keycloak-bootstrap.md`](docs/keycloak-bootstrap.md) — the managed Keycloak a VPN cluster bootstraps

Architecture and background:

- [`docs/architecture.md`](docs/architecture.md) — how the CLI is put together
- [`docs/netbird-vpn-architecture.md`](docs/netbird-vpn-architecture.md) — the NetBird mesh around the clusters
- [`docs/hetzner-hcloud-vpn-cluster.md`](docs/hetzner-hcloud-vpn-cluster.md) — the HCloud VPN-cluster topology
- [`docs/bare-metal-provisioning.md`](docs/bare-metal-provisioning.md) — how a Hetzner bare-metal node gets provisioned end to end

## Development

See [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) for setup instructions.

### Requirements

- [Nix](https://github.com/DeterminateSystems/nix-installer) and [Direnv](https://direnv.net/)
- Docker

### Building

```sh
# Build the kubeaid-cli binary
make build

# Build the kubeaid-storagectl binary (bare-metal storage helper)
make build-storagectl

# Lint and format
make lint
make format

# Run unit tests with coverage
make test
```

Run `make help` to list every target.

## Contributing

Contributions are welcome.

1. Open an [issue](https://github.com/Obmondo/kubeaid-cli/issues) describing the bug or feature before starting substantial work.
2. Follow Google's [Go style guide](https://google.github.io/styleguide/go/decisions); run `make lint` and `make format` before pushing (CI is strict).
3. Write [Conventional Commits](https://www.conventionalcommits.org/) — releases are cut with [cocogitto](https://docs.cocogitto.io/).
4. Open a pull request that references the issue and explains the *why*, not just the *what*.

See [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) for the local development setup.

## License

[GNU Affero General Public License v3.0](LICENSE)
