#!/bin/bash

set -o verbose
set -o errexit
set -o nounset # Causes the shell to treat unset variables as errors and exit immediately.

apt update

# -------------------------- Required by KubeAid to build KubePrometheus --------------------------

apt install -y jsonnet jq gnupg2 scdaemon curl

# gojsontoyaml
wget https://github.com/brancz/gojsontoyaml/releases/download/v0.1.0/gojsontoyaml_0.1.0_linux_"${CPU_ARCHITECTURE}".tar.gz
tar -xvzf gojsontoyaml_0.1.0_linux_"${CPU_ARCHITECTURE}".tar.gz
chmod +x gojsontoyaml
mkdir -p /usr/local/bin
mv ./gojsontoyaml /usr/local/bin

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

# ------------------------------------------- Utilities -------------------------------------------

# K9s
wget https://github.com/derailed/k9s/releases/download/v0.32.5/k9s_linux_"${CPU_ARCHITECTURE}".deb
dpkg -i k9s_linux_"${CPU_ARCHITECTURE}".deb
rm k9s_linux_"${CPU_ARCHITECTURE}".deb

apt install -y vim

# ------------------------------------------ Add SSH keys -----------------------------------------
mkdir -p /root/.ssh
ssh-keyscan {github.com,gitlab.com} >>/root/.ssh/known_hosts
ssh-keyscan -p 2223 gitea.obmondo.com >>/root/.ssh/known_hosts
