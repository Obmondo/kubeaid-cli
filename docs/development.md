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
