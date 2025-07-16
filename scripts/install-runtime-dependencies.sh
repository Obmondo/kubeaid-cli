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

# GoJsonToYAML
GO_JSON_TO_YAML_VERSION="0.1.0"
wget https://github.com/brancz/gojsontoyaml/releases/download/v"${GO_JSON_TO_YAML_VERSION}"/gojsontoyaml_"${GO_JSON_TO_YAML_VERSION}"_linux_"${CPU_ARCHITECTURE}".tar.gz
tar -xvzf gojsontoyaml_"${GO_JSON_TO_YAML_VERSION}"_linux_"${CPU_ARCHITECTURE}".tar.gz
chmod +x gojsontoyaml
mkdir -p /usr/local/bin
mv ./gojsontoyaml /usr/local/bin

# JB (jsonnet bundler)
JB_VERSION="v0.6.0"
wget https://github.com/jsonnet-bundler/jsonnet-bundler/releases/download/${JB_VERSION}/jb-linux-${CPU_ARCHITECTURE}
chmod +x jb-linux-${CPU_ARCHITECTURE}
mv jb-linux-${CPU_ARCHITECTURE} /usr/local/bin/jb

# ------------------------------ Required by KubeAid Bootstrap Script -----------------------------

# Kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${CPU_ARCHITECTURE}/kubectl"
chmod +x ./kubectl
mv ./kubectl /usr/local/bin

# KubeOne
KUBEONE_VERSION=$(curl -w '%{url_effective}' -I -L -s -S https://github.com/kubermatic/kubeone/releases/latest -o /dev/null | sed -e 's|.*/v||')
apt-get install -y unzip
curl -LO "https://github.com/kubermatic/kubeone/releases/download/v${KUBEONE_VERSION}/kubeone_${KUBEONE_VERSION}_linux_amd64.zip"
unzip kubeone_${KUBEONE_VERSION}_linux_amd64.zip -d kubeone_${KUBEONE_VERSION}_linux_amd64
mv kubeone_${KUBEONE_VERSION}_linux_amd64/kubeone /usr/local/bin

# Cilium CLI
CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)
curl -L --remote-name-all https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CPU_ARCHITECTURE}.tar.gz{,.sha256sum}
sha256sum --check cilium-linux-${CPU_ARCHITECTURE}.tar.gz.sha256sum
tar -C /usr/local/bin -xzvf cilium-linux-${CPU_ARCHITECTURE}.tar.gz
rm cilium-linux-${CPU_ARCHITECTURE}.tar.gz{,.sha256sum}
