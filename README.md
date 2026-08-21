# KubeAid CLI

[![Release](https://github.com/Obmondo/kubeaid-cli/actions/workflows/release.yaml/badge.svg)](https://github.com/Obmondo/kubeaid-cli/actions/workflows/release.yaml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/Obmondo/kubeaid-cli?sort=semver)](https://github.com/Obmondo/kubeaid-cli/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/Obmondo/kubeaid-cli)](https://goreportcard.com/report/github.com/Obmondo/kubeaid-cli)
[![Docs](https://img.shields.io/badge/docs-kubeaid.io-blue)](https://kubeaid.io/docs/kubeaid-cli/architecture)

KubeAid CLI operates the full lifecycle of [KubeAid](https://github.com/Obmondo/KubeAid)-managed Kubernetes clusters — bootstrap, upgrade, recover, test, and delete — across AWS (self-managed or EKS), Azure (self-managed or AKS), Hetzner, and bare metal, the GitOps-native way.

It is the entry point to the KubeAid platform: the CLI consumes the [KubeAid repository](https://github.com/Obmondo/KubeAid) — curated, vendored Helm charts, monitoring, and secure defaults, delivered as regular reviewed updates — so you don't carry the mental overhead of tracking what's broken, deprecated, or current best practice across the Kubernetes ecosystem.

## Table of contents

- [Architecture](#architecture)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Cloud providers](#cloud-providers)
- [Configuration](#configuration)
- [Documentation](#documentation)

## Architecture

KubeAid CLI is a **single self-contained binary**. The only local requirement is **Docker**, used to run a local [K3D](https://k3d.io/) cluster.

How it provisions depends on the target:

- **Cluster API clouds** — **AWS** (CAPA, self-managed or a managed EKS control plane), **Azure** (CAPZ + Crossplane, self-managed or a managed AKS control plane), and **Hetzner** (CAPH): it stands up a throwaway **K3D management cluster**, installs Cluster API there, provisions your target cluster, then `clusterctl move` **pivots** every Cluster API resource onto the target so it self-manages and the K3D cluster is discarded.
- **Generic bare metal** — **KubeOne** installs Kubernetes straight onto your hosts, with no K3D or Cluster API.
- **Local** — the K3D cluster is simply the cluster itself.

From there it is **GitOps**. The engine renders your `general.yaml` into manifests and commits them to your own **KubeAid Config** repo that overrides only the genuine differences on top of the upstream [KubeAid](https://github.com/Obmondo/KubeAid) platform defaults; [ArgoCD](https://argo-cd.readthedocs.io/) on the target then reconciles the addon stack — Cilium, cert-manager, kube-prometheus, Rook-Ceph, Velero, Sealed Secrets, and more. For the full breakdown, see [`docs/architecture.md`](docs/architecture.md).

## Features

- **Cluster lifecycle management** — bootstrap, upgrade, recover, test, and delete Kubernetes clusters
- **Backup status reporting** — `backup status` shows CNPG and Velero backup health at a glance, see [`docs/backup-status.md`](docs/backup-status.md)
- **Development environments** — spin up local K3D-based dev clusters
- **Multi-cloud support** — AWS, Azure, Hetzner (cloud, bare-metal, hybrid), and generic bare-metal, including managed EKS and AKS control planes
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

**Docker** — installed and running. The CLI uses it for the local K3D cluster; everything else (Helm, K3D,
clusterctl, KubeOne) is embedded in the binary.

**SSH access to your Git repos** — the CLI pushes to your kubeaid-config repository over SSH. Pick one of two
auth methods in `general.yaml`:

| Method | When | `general.yaml` |
|---|---|---|
| ssh-agent | passphrase / YubiKey keys | `git.useSSHAgent: true` |
| Key file | unencrypted key on disk | `git.privateKeyFilePath` |

Exactly one of the two must be set.

## Quick start

1. Walk through the interactive prompt to generate `general.yaml` and `secrets.yaml`:

   ```sh
   kubeaid-cli config generate
   ```

2. Review the generated files under `~/.config/kubeaid-cli/<cluster>/configs/` (the prompt covers everything
   required to bootstrap; hand-edit only when you want to override defaults).

3. Bootstrap the cluster:

   ```sh
   kubeaid-cli cluster bootstrap
   ```

   The config is found under `~/.config/kubeaid-cli/<cluster>/configs/` automatically (pass
   `--cluster-name <cluster>` to pick one non-interactively, or `--configs-directory` if you keep the config
   somewhere else). `cluster bootstrap` fails fast if the configs are missing — run `config generate` first.

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
| `--cluster-name` | Cluster whose config to use, from `~/.config/kubeaid-cli/<name>/configs` |
| `--configs-directory` | Path to directory containing `general.yaml` and `secrets.yaml` (overrides `--cluster-name`) |

## Cloud providers

| Provider | Bootstrap | Upgrade | Recover | Delete |
|---|---|---|---|---|
| AWS | Yes | Yes | Yes | Yes |
| AWS EKS (managed) | Yes | GitOps¹ | —² | Yes |
| Azure | Yes | Yes | Yes | Yes |
| Azure AKS (managed) | Yes | GitOps¹ | —² | Yes |
| Hetzner Cloud | Yes | WIP | WIP | Yes |
| Hetzner Bare Metal | Yes | WIP | WIP | Yes |
| Hetzner Hybrid | Yes | WIP | WIP | Yes |
| Bare Metal | Yes | Yes | — | Yes |
| Local (K3D) | Yes | — | — | — |

`WIP` — work in progress; landing soon, not yet generally available.

¹ Managed control planes aren't upgraded via `cluster upgrade`: bump `global.kubernetes.version` in your
kubeaid-config repo instead — CAPA/CAPZ then upgrades the control plane and rolls the node groups / agent pools.

² `cluster recover` isn't wired for managed control planes yet — re-bootstrap and restore from the Velero backup
manually.

## Kubernetes version support

Requested Kubernetes versions are validated at bootstrap: released, not past end-of-life, and within your
provider's supported range. See [`docs/kubernetes-version-support.md`](docs/kubernetes-version-support.md).

## Configuration

KubeAid CLI uses two YAML config files:

- **`general.yaml`** — cluster settings, cloud provider config, ArgoCD deploy keys, Git repo URLs, node groups, etc.
- **`secrets.yaml`** — cloud credentials, tokens, and other sensitive values.

See [`docs/config-reference.md`](docs/config-reference.md) for the full configuration reference.

## Documentation

**Day-to-day operator guides**

- [Post-bootstrap checklist](docs/post-bootstrap.md) — what to do right after a cluster comes up
- [Backup status](docs/backup-status.md) — check CNPG and Velero backup health via backup-exporter
- [Add a bare-metal worker](docs/add-bare-metal-worker.md) — grow a Hetzner bare-metal worker pool (see also the [manual git-only flow](docs/add-bare-metal-worker-manual.md))
- [Upgrade a bare-metal cluster](docs/upgrade-bare-metal.md) — bump the Kubernetes version of a bare-metal (KubeOne) cluster
- [Troubleshooting](docs/troubleshooting.md) — recovery paths for recurring bootstrap failures (Hetzner, Sealed Secrets, ArgoCD)

**Identity and SSO**

- [Keycloak bootstrap](docs/keycloak-bootstrap.md) — the managed Keycloak a VPN cluster bootstraps

**Architecture and background**

- [Architecture](docs/architecture.md) — how the CLI is put together
- [NetBird VPN architecture](docs/netbird-vpn-architecture.md) — the NetBird mesh around the clusters
- [Hetzner HCloud VPN cluster](docs/hetzner-hcloud-vpn-cluster.md) — the HCloud VPN-cluster topology
- [Bare-metal provisioning](docs/bare-metal-provisioning.md) — how a Hetzner bare-metal node gets provisioned end to end
- [Hetzner bare-metal network surface](docs/hetzner-bare-metal-network-surface.md) — the Cilium host-firewall policy locking down each node's public NIC

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

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for dev
setup, code standards, and how to open a pull request.

## Community and governance

- [Code of Conduct](CODE_OF_CONDUCT.md) — we follow the CNCF Community Code of Conduct.
- [Governance](GOVERNANCE.md) — how decisions are made and how maintainers are added.
- [Maintainers](MAINTAINERS.md) — current maintainers of the project.
- [Adopters](ADOPTERS.md) — organizations running KubeAid CLI; add yours with a PR.
- [Security policy](SECURITY.md) — how to report vulnerabilities privately.

## License

[Apache License, Version 2.0](LICENSE)
