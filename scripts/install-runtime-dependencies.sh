#!/bin/bash

# -------------------------------- Control flow -------------------------------

set -o verbose
set -o errexit
set -o nounset # Causes the shell to treat unset variables as errors and exit immediately.

if ! command -v wget &>/dev/null; then
  echo "🚨 Error: wget is not installed."
  exit 1
fi

if ! command -v curl &>/dev/null; then
  echo "🚨 Error: curl is not installed."
  exit 1
fi

if ! command -v unzip &>/dev/null; then
  echo "🚨 Error: unzip is not installed."
  exit 1
fi

CLOUD_PROVIDER="${CLOUD_PROVIDER:-local}"
BINARY_DESTINATION="${BINARY_DESTINATION:-/usr/local/bin}"

OS=$([ "$(uname -s)" = "Linux" ] && echo "linux" || echo "darwin")
CPU_ARCHITECTURE=$([ "$(uname -m)" = "x86_64" ] && echo "amd64" || echo "arm64")

# Create /tmp/kubeaid-bootstrap-script.
# We will do everything here, and then at the end, cleanup by removing this directory.
mkdir -p /tmp/kubeaid-bootstrap-script
cd /tmp/kubeaid-bootstrap-script

mkdir -p "${BINARY_DESTINATION}"

# -------------------------------------------- Functions --------------------------------------------

# Check if the dependency already exists
dep_exists() {
  if command -v "$1" >/dev/null 2>&1; then
    echo "$1 is installed already."
    return 0
  fi

  echo "$1 is not installed. Installing..."
  return 1
}

install_jsonnet() {
  local binary_name="jsonnet"
  if dep_exists "${binary_name}"; then
    return
  fi

  JSONNET_VERSION=$(curl --retry 5 --retry-connrefused -w '%{url_effective}' -I -L -s -S https://github.com/google/go-jsonnet/releases/latest -o /dev/null | sed -e 's|.*/v||')
  JSONNET_DOWNLOAD_URL=https://github.com/google/go-jsonnet/releases/download/v"${JSONNET_VERSION}"/go-jsonnet_"${JSONNET_VERSION}"_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz

  wget --tries=5 "${JSONNET_DOWNLOAD_URL}" -O "${binary_name}".tar.gz
  tar -C "${BINARY_DESTINATION}" -xzvf "${binary_name}".tar.gz

  rm "${BINARY_DESTINATION}"/jsonnet-deps "${BINARY_DESTINATION}"/jsonnet-lint "${BINARY_DESTINATION}"/jsonnetfmt "${BINARY_DESTINATION}"/LICENSE "${BINARY_DESTINATION}"/README.md
}

install_jq() {
  local binary_name="jq"
  if dep_exists "${binary_name}"; then
    return
  fi

  JQ_VERSION=$(curl --retry 5 --retry-connrefused -w '%{url_effective}' -I -L -s -S https://github.com/jqlang/jq/releases/latest -o /dev/null | sed -e 's|.*/jq-||')
  JQ_DOWNLOAD_URL=https://github.com/jqlang/jq/releases/download/jq-"${JQ_VERSION}"/jq-"${OS}"-"${CPU_ARCHITECTURE}"
  if [[ "$OS" == "darwin" ]]; then
    JQ_DOWNLOAD_URL=https://github.com/jqlang/jq/releases/download/jq-"${JQ_VERSION}"/jq-macos-"${CPU_ARCHITECTURE}"
  fi

  wget --tries=5 "${JQ_DOWNLOAD_URL}" -O "${binary_name}"
  chmod +x "${binary_name}"
  mv "${binary_name}" "${BINARY_DESTINATION}"
}

install_gojsontoyaml() {
  local binary_name="gojsontoyaml"
  if dep_exists "${binary_name}"; then
    return
  fi

  GO_JSON_TO_YAML_VERSION=$(curl --retry 5 --retry-connrefused -w '%{url_effective}' -I -L -s -S https://github.com/brancz/gojsontoyaml/releases/latest -o /dev/null | sed -e 's|.*/v||')
  wget --tries=5 https://github.com/brancz/gojsontoyaml/releases/download/v"${GO_JSON_TO_YAML_VERSION}"/gojsontoyaml_"${GO_JSON_TO_YAML_VERSION}"_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz
  tar -xvzf gojsontoyaml_"${GO_JSON_TO_YAML_VERSION}"_"${OS}"_"${CPU_ARCHITECTURE}".tar.gz
  chmod +x gojsontoyaml
  mv ./gojsontoyaml "${BINARY_DESTINATION}"
}

install_jb() {
  local binary_name="jb"
  if dep_exists "${binary_name}"; then
    return
  fi

  JB_VERSION=$(curl --retry 5 --retry-connrefused -w '%{url_effective}' -I -L -s -S https://github.com/jsonnet-bundler/jsonnet-bundler/releases/latest -o /dev/null | sed -e 's|.*/v||')
  wget --tries=5 https://github.com/jsonnet-bundler/jsonnet-bundler/releases/download/v"${JB_VERSION}"/jb-"${OS}"-"${CPU_ARCHITECTURE}"
  chmod +x jb-"${OS}"-"${CPU_ARCHITECTURE}"
  mv jb-"${OS}"-"${CPU_ARCHITECTURE}" "${BINARY_DESTINATION}"/jb
}

install_kubectl() {
  local binary_name="kubectl"
  if dep_exists "${binary_name}"; then
    return
  fi

  curl --retry 5 --retry-connrefused -LO "https://dl.k8s.io/release/$(curl --retry 5 --retry-connrefused -L -s https://dl.k8s.io/release/stable.txt)/bin/"${OS}"/${CPU_ARCHITECTURE}/kubectl"
  chmod +x ./kubectl
  mv ./kubectl "${BINARY_DESTINATION}"
}

install_cilium() {
  local binary_name="cilium"
  if dep_exists "${binary_name}"; then
    return
  fi

  CILIUM_CLI_VERSION=$(curl --retry 5 --retry-connrefused --retry-all-errors -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)

  echo "Installing cilium CLI version ${CILIUM_CLI_VERSION}..."

  TAR_URL="https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz"
  SHA_URL="https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz.sha256sum"

  # Try wget first (often handles DNS better in containers)
  if command -v wget &>/dev/null; then
    echo "Using wget for cilium download..."
    wget --tries=3 --retry-connrefused --timeout=30 \
      -O cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz \
      "${TAR_URL}" || {
      echo "wget failed, falling back to curl..."
      curl -L --retry 3 --retry-connrefused --retry-all-errors \
        -o cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz \
        "${TAR_URL}"
    }

    wget --tries=3 --retry-connrefused --timeout=30 \
      -O cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz.sha256sum \
      "${SHA_URL}" 2>/dev/null || echo "Warning: SHA256 file download failed, skipping checksum"
  else
    # Fallback to curl with GitHub redirect
    curl -L --retry 3 --retry-connrefused --retry-all-errors \
      -o cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz \
      "${TAR_URL}"

    curl -L --retry 3 --retry-connrefused --retry-all-errors \
      -o cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz.sha256sum \
      "${SHA_URL}" 2>/dev/null || echo "Warning: SHA256 file download failed, skipping checksum"
  fi

  # Only verify checksum if SHA file was downloaded
  if [ -f cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz.sha256sum ]; then
    sha256sum -c cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz.sha256sum
  else
    echo "Warning: Skipping checksum verification (SHA256 file unavailable)"
  fi

  tar -C "${BINARY_DESTINATION}" -xzvf cilium-${OS}-${CPU_ARCHITECTURE}.tar.gz
}

# -------------------------------- Required to build KubePrometheus -------------------------------

install_jsonnet
install_jq
install_gojsontoyaml
install_jb

# --------------------------- Required solely by KubeAid Bootstrap Script -------------------------

install_kubectl

install_cilium

# -------------------------------------------- Cleanup --------------------------------------------

# Remove /tmp/kubeaid-bootstrap-script.
rm -rf /tmp/kubeaid-bootstrap-script
