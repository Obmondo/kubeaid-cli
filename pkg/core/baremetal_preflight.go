// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"time"

	"k8c.io/kubeone/pkg/executor"
	kubeonessh "k8c.io/kubeone/pkg/ssh"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

// kubeAPIServerLocalPort is kubeadm's local bind port for the kube-apiserver. The cluster
// endpoint port may differ (load-balancer), but on the control-plane host itself kubeadm /
// KubeOne always bind 6443.
const kubeAPIServerLocalPort = 6443

// assertControlPlaneHostsNotHalfInitialized SSHes into every Bare Metal control-plane host and
// fails fast when a previous 'kubeadm init' died partway : /etc/kubernetes/admin.conf exists,
// but no kube-apiserver listens locally. KubeOne's own init guard treats admin.conf as "the
// cluster is healthy", skips 'kubeadm init' and wedges retrying 'kubeadm token create' against
// the dead apiserver. Healthy hosts (admin.conf + live apiserver) and fresh hosts (no
// admin.conf) pass.
func assertControlPlaneHostsNotHalfInitialized(ctx context.Context) {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal
	connector := kubeonessh.NewConnector(ctx)

	for _, host := range bareMetalConfig.ControlPlane.Hosts {
		connection := sshIntoBareMetalHost(ctx, host, connector)

		// Non-zero exit (surfaced as an error) → no admin.conf → fresh host.
		if _, _, _, err := connection.Exec("test -f /etc/kubernetes/admin.conf"); err != nil {
			connection.Close()
			continue
		}

		// Probe the apiserver from the host's own viewpoint : a TCP dial through the SSH
		// tunnel to the host's localhost, so operator-side firewalls / the Cilium host
		// firewall can't skew the result. Every KubeOne-managed host already permits SSH
		// TCP forwarding - KubeOne itself tunnels its Kubernetes client this way.
		err := dialThroughHost(ctx, connection, kubeAPIServerLocalPort)
		connection.Close()

		hostAddress := bareMetalHostAddress(host)
		assert.Assert(ctx, err == nil, fmt.Sprintf(
			`Control-plane host %s is half-initialized : /etc/kubernetes/admin.conf exists, but no kube-apiserver is listening on port %d.
A previous 'kubeadm init' died partway - KubeOne would treat the host as healthy and wedge retrying 'kubeadm token create' against the dead apiserver.

Clean the host and rerun :

  ssh root@%s
  kubeadm reset -f
  rm -rf /etc/kubernetes /var/lib/etcd`,
			hostAddress, kubeAPIServerLocalPort, hostAddress,
		))
	}
}

// assertBareMetalHostsPackageStateHealthy SSHes into every Bare Metal host and fails fast
// when the package manager state is broken ('apt-get check' fails) - an interrupted install
// leaves unmet dependencies, and KubeOne's very first 'apt-get install' would die with
// 'E: Unmet dependencies' minutes in. Non-Debian hosts (no apt-get) are skipped.
func assertBareMetalHostsPackageStateHealthy(ctx context.Context) {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal
	connector := kubeonessh.NewConnector(ctx)

	hosts := []*config.BareMetalHost{}
	hosts = append(hosts, bareMetalConfig.ControlPlane.Hosts...)
	for _, nodeGroup := range bareMetalConfig.NodeGroups {
		hosts = append(hosts, nodeGroup.Hosts...)
	}

	for _, host := range hosts {
		connection := sshIntoBareMetalHost(ctx, host, connector)

		_, stderr, _, err := connection.Exec(
			"! command -v apt-get >/dev/null 2>&1 || sudo apt-get check -qq",
		)
		connection.Close()

		hostAddress := bareMetalHostAddress(host)
		assert.Assert(ctx, err == nil, fmt.Sprintf(
			`Package manager state on host %s is broken ('apt-get check' failed) - an interrupted install left unmet dependencies, and KubeOne's package installation would die on it.

Fix the host and rerun :

  ssh root@%s
  dpkg --configure -a
  apt --fix-broken install

%s`,
			hostAddress, hostAddress, stderr,
		))
	}
}

// dialThroughHost TCP-dials the host's own 127.0.0.1:<port> through the SSH connection's
// tunnel. nil means something is listening on that port, from the host's point of view.
func dialThroughHost(ctx context.Context, connection executor.Interface, port int) error {
	tunneler, ok := connection.(executor.Tunneler)
	if !ok {
		return fmt.Errorf("SSH connection doesn't support tunneling")
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := tunneler.TunnelTo(dialCtx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	return conn.Close()
}
