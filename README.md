# KubeAid CLI

[![Release](https://github.com/Obmondo/kubeaid-cli/actions/workflows/release.yaml/badge.svg)](https://github.com/Obmondo/kubeaid-cli/actions/workflows/release.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Obmondo/kubeaid-cli)](https://goreportcard.com/report/github.com/Obmondo/kubeaid-cli)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

KubeAid CLI helps you operate [KubeAid](https://github.com/Obmondo/kubeaid) managed Kubernetes cluster lifecycle in a GitOps native way.

## Architecture

KubeAid CLI is a thin client that proxies `cluster` and `devenv` commands to a containerized **KubeAid Core** engine. It pulls the matching `ghcr.io/obmondo/kubeaid-core` image, sets up Docker networking, mounts config files and SSH keys, and streams output back to your terminal.

## Features

- **Cluster lifecycle management** — bootstrap, upgrade, recover, test, and delete Kubernetes clusters
- **Development environments** — spin up local K3D-based dev clusters
- **Multi-cloud support** — AWS, Azure, Hetzner (cloud, bare-metal, hybrid), and generic bare-metal
- **GitOps native** — integrates with ArgoCD, KubeAid Config repos, and sealed secrets
- **Config generation** — generate sample configuration files per cloud provider

## Installation

### Shell script (Linux / macOS)

```sh
curl -fsSL https://raw.githubusercontent.com/Obmondo/kubeaid-cli/main/install.sh | sh
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
go install github.com/Obmondo/kubeaid-bootstrap-script/cmd/kubeaid-cli@latest
```

## Prerequisites

- **Docker** — must be installed and running (KubeAid Core runs as a container)
- **SSH agent** — `ssh-agent` with your keys loaded (`SSH_AUTH_SOCK` must be set)

## Quick start

1. Generate sample config files for your cloud provider:

   ```sh
   kubeaid-cli config generate <aws|azure|hetzner|bare-metal|local>
   ```

2. Edit the generated `general.yaml` and `secrets.yaml` in the output directory.

3. Bootstrap a cluster:

   ```sh
   kubeaid-cli cluster bootstrap --configs-directory ./outputs/configs/<provider>/
   ```

## Usage

```
kubeaid-cli [command] [flags]
```

### Commands

| Command | Description |
|---|---|
| `config generate <provider>` | Generate sample general and secrets config files |
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
| Hetzner Cloud | Yes | — | — | Yes |
| Hetzner Bare Metal | Yes | — | — | Yes |
| Hetzner Hybrid | Yes | — | — | Yes |
| Bare Metal | Yes | — | — | Yes |
| Local (K3D) | Yes | — | — | — |

## Configuration

KubeAid CLI uses two YAML config files:

- **`general.yaml`** — cluster settings, cloud provider config, ArgoCD deploy keys, Git repo URLs, node groups, etc.
- **`secrets.yaml`** — cloud credentials, tokens, and other sensitive values.

See [`docs/config-reference.md`](docs/config-reference.md) for the full configuration reference.

## Development

See [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) for setup instructions.

### Requirements

- [Nix](https://github.com/DeterminateSystems/nix-installer) and [Direnv](https://direnv.net/)
- Docker

### Building

```sh
# Build the CLI binary
make build-cli

# Build the KubeAid Core container image
make build-image

# Run linter
make lint
```

## License

[GNU Affero General Public License v3.0](LICENSE)
