#!/bin/bash

set -o verbose
set -o errexit
set -o nounset # Causes the shell to treat unset variables as errors and exit immediately

apt update

# Jsonnet and jq.
apt install -y jsonnet jq

# Kubeseal.
KUBESEAL_VERSION="0.23.0"
curl -OL "https://github.com/bitnami-labs/sealed-secrets/releases/download/v${KUBESEAL_VERSION:?}/kubeseal-${KUBESEAL_VERSION:?}-linux-${CPU_ARCHITECTURE}.tar.gz"
tar -xvzf kubeseal-${KUBESEAL_VERSION:?}-linux-"${CPU_ARCHITECTURE}".tar.gz kubeseal
install -m 755 kubeseal /usr/local/bin/kubeseal

# gojsontoyaml.
wget https://github.com/brancz/gojsontoyaml/releases/download/v0.1.0/gojsontoyaml_0.1.0_linux_"${CPU_ARCHITECTURE}".tar.gz
tar -xvzf gojsontoyaml_0.1.0_linux_"${CPU_ARCHITECTURE}".tar.gz
chmod +x gojsontoyaml
mkdir -p /usr/local/bin
mv ./gojsontoyaml /usr/local/bin

# K3d.
apt install -y curl
curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash

# Clusterawsadm.
wget https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases/download/v2.5.2/clusterawsadm_v2.5.2_linux_"${CPU_ARCHITECTURE}"
mv clusterawsadm_v2.5.2_linux_"${CPU_ARCHITECTURE}" /usr/local/bin/clusterawsadm
chmod +x /usr/local/bin/clusterawsadm

# Kubectl.
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${CPU_ARCHITECTURE}/kubectl"
chmod +x ./kubectl
mv ./kubectl /usr/local/bin

# Helm.
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
chmod 700 get_helm.sh
./get_helm.sh

# ArgoCD.
curl -sSL -o argocd-linux-"${CPU_ARCHITECTURE}" https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-"${CPU_ARCHITECTURE}"
install -m 555 argocd-linux-"${CPU_ARCHITECTURE}" /usr/local/bin/argocd
rm argocd-linux-"${CPU_ARCHITECTURE}"

# Clusterctl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.7.3/clusterctl-linux-${CPU_ARCHITECTURE} -o clusterctl
install -o root -g root -m 0755 clusterctl /usr/local/bin/clusterctl

# K3d
wget https://github.com/derailed/k9s/releases/download/v0.32.5/k9s_linux_"${CPU_ARCHITECTURE}".deb
dpkg -i k9s_linux_"${CPU_ARCHITECTURE}".deb
rm k9s_linux_"${CPU_ARCHITECTURE}".deb
