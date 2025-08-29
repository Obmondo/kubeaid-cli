# Development Guide

## Setup

- Install [Nix](https://github.com/DeterminateSystems/nix-installer) and [Direnv](https://direnv.net/).

- When you `cd` into this Git repository, for the first time, run `direnv allow`.
  From then onwards, whenever you `cd` here, you'll be dropped into a Nix shell with all the required tools for development.

Also, you must have Docker installed and running.

## Bootstrapping a Cluster

Let's try to bootstrap a local K3D cluster!

- Run `make sample-config-generate-local-dev`.
  Sample general and secret config files will be generated for you in `outputs/configs/local`. Go through and adjust them accordingly.

- Run `make bootstrap-cluster-local-dev`.

## Debugging cluster provisioning using ClusterAPI

If cluster provisioning by ClusterAPI gets stuck, then you can :

- check pod logs of ClusterAPI components : `Operator`, `Core Provider`, `Bootstrap Provider`, `ControlPlane Provider` and `Infrastructure Provider`.

- SSH into the control-plane node. Then view cloud-init output logs stored at `/var/log/cloud-init-output.log`.

## GOTCHAs

- If the `clusterawsadm bootstrap iam create-cloudformation-stack` command errors out with this message :

  > the IAM CloudFormation Stack create / update failed and it's currently in a `ROLLBACK_COMPLETE` state

  then that means maybe there are pre-existing IAM resources with overlapping name. Then first delete them manually from the AWS Console and then retry running the script. Filter the IAM roles and policies in the corresponding region with the keyword : `cluster` / `clusterapi`.

## Release Procedure

Checkout to a new branch.

Software versioning is controlled via a single source of truth : `cmd/kubeaid-core/root/version/version.txt`.
Run [standard-version](https://github.com/conventional-changelog/standard-version), using bun, to :

- Bump the version in that version.txt file.

- Update `vendorHash` for the `KubeAid CLI` package in `flake.nix`.
  > This will take a bit of time.
  
- Create a release commit.

- Create a new tag.

Merge the release commit into the main branch, and sync everything to GitHub.

Create a new GitHub release.
> We tried using goreleaser, which would automatically create the release, with changelogs and, build and publish the binaries as release artifacts.
> But then, had to abandon it, due to the `no disk space available` error we were getting in the GitHub Actions release workflow. It was building binaries for all the targets parallelly, in the same GitHub Actions runner.
> Hopefully, we can bridge this gap in the future, by migrating from standard-version to [release-please](https://github.com/googleapis/release-please).

A triggered GitHub Actions release workflow will :

- Scan the source-code, looking for vulnerabilities.
  > You can view the scan result in the GitHub Actions release workflow summary.

- Build and publish new `KubeAid CLI` binaries, as release artifacts.

- Build and publish new `KubeAid Core` container images.
