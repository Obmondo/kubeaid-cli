#!/bin/bash

set -o verbose
set -o errexit
set -o nounset # Causes the shell to treat unset variables as errors and exit immediately.

# Get CPU architecture.
CPU_ARCHITECTURE=$([ "$(uname -m)" = "x86_64" ] && echo "amd64" || echo "arm64")

apt update

apt install -y curl wget

# -------------------------------- Required to build KubePrometheus -------------------------------

apt install -y jsonnet jq

# gojsontoyaml
wget https://github.com/brancz/gojsontoyaml/releases/download/v0.1.0/gojsontoyaml_0.1.0_linux_"${CPU_ARCHITECTURE}".tar.gz
tar -xvzf gojsontoyaml_0.1.0_linux_"${CPU_ARCHITECTURE}".tar.gz
chmod +x gojsontoyaml
mkdir -p /usr/local/bin
mv ./gojsontoyaml /usr/local/bin

# JB jsonnet package manager
JB_VERSION="v0.6.0"
wget https://github.com/jsonnet-bundler/jsonnet-bundler/releases/download/${JB_VERSION}/jb-linux-${CPU_ARCHITECTURE}
chmod +x jb-linux-${CPU_ARCHITECTURE}
mv jb-linux-${CPU_ARCHITECTURE} /usr/local/bin/jb

# ------------------------------ Required by KubeAid Bootstrap Script -----------------------------

# Kubeseal
KUBESEAL_VERSION="0.23.0"
curl -OL "https://github.com/bitnami-labs/sealed-secrets/releases/download/v${KUBESEAL_VERSION:?}/kubeseal-${KUBESEAL_VERSION:?}-linux-${CPU_ARCHITECTURE}.tar.gz"
tar -xvzf kubeseal-${KUBESEAL_VERSION:?}-linux-"${CPU_ARCHITECTURE}".tar.gz kubeseal
install -m 755 kubeseal /usr/local/bin/kubeseal

# K3d
apt install -y curl
curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash

# Clusterawsadm
wget https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases/download/v2.7.1/clusterawsadm-linux-"${CPU_ARCHITECTURE}"
mv clusterawsadm-linux-"${CPU_ARCHITECTURE}" /usr/local/bin/clusterawsadm
chmod +x /usr/local/bin/clusterawsadm

# Clusterctl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.7.3/clusterctl-linux-"${CPU_ARCHITECTURE}" -o clusterctl
install -o root -g root -m 0755 clusterctl /usr/local/bin/clusterctl

# Kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${CPU_ARCHITECTURE}/kubectl"
chmod +x ./kubectl
mv ./kubectl /usr/local/bin

# yq
apt install -y yq

# azwi
AZWI_VERSION="1.5.0"
curl -OL "https://github.com/Azure/azure-workload-identity/releases/download/v${AZWI_VERSION:?}/azwi-v${AZWI_VERSION:?}-linux-${CPU_ARCHITECTURE}.tar.gz"
tar -xvzf azwi-v${AZWI_VERSION:?}-linux-"${CPU_ARCHITECTURE}".tar.gz azwi
install -m 755 azwi /usr/local/bin/azwi

# Azure CLI
apt-get -y update
apt-get install -y apt-transport-https ca-certificates curl gnupg lsb-release

mkdir -p /etc/apt/keyrings
curl -sLS https://packages.microsoft.com/keys/microsoft.asc |
  gpg --dearmor | tee /etc/apt/keyrings/microsoft.gpg >/dev/null
chmod go+r /etc/apt/keyrings/microsoft.gpg

AZ_DIST=$(lsb_release -cs)
echo "Types: deb
URIs: https://packages.microsoft.com/repos/azure-cli/
Suites: ${AZ_DIST}
Components: main
Architectures: $(dpkg --print-architecture)
Signed-by: /etc/apt/keyrings/microsoft.gpg" | tee /etc/apt/sources.list.d/azure-cli.sources

apt-get -y update
apt-get install -y azure-cli

# KubeOne
curl -sfL get.kubeone.io | sh
