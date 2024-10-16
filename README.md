# KubeAid Bootstrap Script

The `KubeAid Bootstrap Script` is used to bootstrap Kubernetes clusters using Cluster API and [KubeAid](https://github.com/Obmondo/KubeAid).

> Currently it only supports bootstrapping self-managed clusters in AWS.

## Official Guides

- [Bootstrapping a self-managed cluster in AWS](https://github.com/Obmondo/KubeAid/blob/master/docs/aws/capi/cluster.md)

## Developer Guide

> Make sure, you've Docker installed in your system.

Run `make build-image-dev` to build the KubeAid Bootstrap Script container image (development version).

Then run `make run-container-dev` to run the container.

In a separate terminal window, use `make exec-container-dev` to execute into the container.

Once you're inside the container, use `make generate-sample-config-aws-dev` to generate a sample config file at [./outputs/kubeaid-bootstrap-script.config.yaml](./outputs/kubeaid-bootstrap-script.config.yaml), targetting the AWS cloud provider. Adjust the config file according to your needs.

Then run `make bootstrap-cluster-dev` to bootstrap the cluster!

## REFERENCES

- [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/#comparison-with-client-side-apply)
