#!/bin/bash

set -o verbose
set -o errexit
set -o nounset # Causes the shell to treat unset variables as errors and exit immediately.

if ! command -v wget &> /dev/null; then
    echo "Error: wget is not installed."
    exit 1
fi

if ! command -v curl &> /dev/null; then
    echo "Error: curl is not installed."
    exit 1
fi

if ! command -v unzip &> /dev/null; then
    echo "Error: wget is not installed."
    exit 1
fi

CLOUD_PROVIDER="${CLOUD_PROVIDER:-local}"

OS=$([ "$(uname -s)" = "Linux" ] && echo "linux" || echo "darwin")
# Get CPU architecture.
CPU_ARCHITECTURE=$([ "$(uname -m)" = "x86_64" ] && echo "amd64" || echo "arm64")

# Create /tmp/kubeaid-bootstrap-script.
# We will do everything here, and then at the end, cleanup by removing this directory.
mkdir /tmp/kubeaid-bootstrap-script
cd /tmp/kubeaid-bootstrap-script

# -------------------------------- Required to build KubePrometheus -------------------------------

# Jsonnet and jq.
JSONNET_VERSION=$(curl -w '%{url_effective}' -I -L -s -S https://github.com/google/go-jsonnet/releases/latest -o /dev/null | sed -e 's|.*/v||')
wget https://github.com/google/go-jsonnet/releases/download/v"${JSONNET_VERSION}"/go-jsonnet_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz
tar -C /usr/local/bin -xzvf go-jsonnet_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz

JQ_VERSION=$(curl -w '%{url_effective}' -I -L -s -S https://github.com/jqlang/jq/releases/latest -o /dev/null | sed -e 's|.*/jq-||')
if [[ "$OS" == "linux" ]]; then
  wget https://github.com/jqlang/jq/releases/download/jq-"${JQ_VERSION}"/jq-linux-"${CPU_ARCHITECTURE}"
  mv jq-linux-arm64 /usr/local/bin/jq
else
  wget https://github.com/jqlang/jq/releases/download/jq-"${JQ_VERSION}"/jq-macos-"${CPU_ARCHITECTURE}"
  mv jq-macos-arm64 /usr/local/bin/jq
fi

# GoJsonToYAML
GO_JSON_TO_YAML_VERSION="0.1.0"
wget https://github.com/brancz/gojsontoyaml/releases/download/v"${GO_JSON_TO_YAML_VERSION}"/gojsontoyaml_"${GO_JSON_TO_YAML_VERSION}"_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz
tar -xvzf gojsontoyaml_"${GO_JSON_TO_YAML_VERSION}"_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz
chmod +x gojsontoyaml
mkdir -p /usr/local/bin
mv ./gojsontoyaml /usr/local/bin

# JB (jsonnet bundler)
JB_VERSION="v0.6.0"
wget https://github.com/jsonnet-bundler/jsonnet-bundler/releases/download/${JB_VERSION}/jb-"${OS}"-${CPU_ARCHITECTURE}
chmod +x jb-"${OS}"-${CPU_ARCHITECTURE}
mv jb-"${OS}"-${CPU_ARCHITECTURE} /usr/local/bin/jb

# --------------------------- Required solely by KubeAid Bootstrap Script -------------------------

# Kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/"${OS}"/${CPU_ARCHITECTURE}/kubectl"
chmod +x ./kubectl
mv ./kubectl /usr/local/bin

if [[ "$CLOUD_PROVIDER" == "bare-metal" ]]; then
  # KubeOne
  KUBEONE_VERSION=$(curl -w '%{url_effective}' -I -L -s -S https://github.com/kubermatic/kubeone/releases/latest -o /dev/null | sed -e 's|.*/v||')
  curl -LO "https://github.com/kubermatic/kubeone/releases/download/v${KUBEONE_VERSION}/kubeone_${KUBEONE_VERSION}_"${OS}"_"${CPU_ARCHITECTURE}".zip"
  unzip kubeone_${KUBEONE_VERSION}_"${OS}"_"${CPU_ARCHITECTURE}".zip -d kubeone_${KUBEONE_VERSION}_"${OS}"_"${CPU_ARCHITECTURE}"
  mv kubeone_${KUBEONE_VERSION}_"${OS}"_"${CPU_ARCHITECTURE}"/kubeone /usr/local/bin
fi

# Cilium CLI
CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)
curl -OL --remote-name-all https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-"${OS}"-${CPU_ARCHITECTURE}.tar.gz{,.sha256sum}
sha256sum --check cilium-"${OS}"-${CPU_ARCHITECTURE}.tar.gz.sha256sum
tar -C /usr/local/bin -xzvf cilium-"${OS}"-${CPU_ARCHITECTURE}.tar.gz

# -------------------------------------------- Cleanup --------------------------------------------

# Remove /tmp/kubeaid-bootstrap-script.
rm -rf /tmp/kubeaid-bootstrap-script
