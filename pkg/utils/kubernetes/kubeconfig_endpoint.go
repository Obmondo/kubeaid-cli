// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// PointMainClusterKubeconfigAtHostname makes the main cluster's kubeconfig
// dial the control-plane hostname again, undoing the bootstrap-time switch to
// the LB's public IP. Call it once the LB's public interface is disabled: the
// operator then reaches the hostname's private IP over the NetBird mesh.
// No-op when the kubeconfig doesn't exist or doesn't dial through an IP.
func PointMainClusterKubeconfigAtHostname(ctx context.Context) error {
	path := mainClusterKubeconfigPath()

	kubeconfig, err := loadKubeConfigFromFileFn(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed reading main cluster's kubeconfig file: %w", err)
	}

	changed, err := dialByName(kubeconfig)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if err := clientcmd.WriteToFile(*kubeconfig, path); err != nil {
		return fmt.Errorf("failed writing main cluster's kubeconfig file: %w", err)
	}
	slog.InfoContext(ctx, "Main cluster's kubeconfig now dials the control-plane hostname")
	return nil
}

// kubeconfigDialingThroughIP returns raw with every cluster whose server host
// is hostname pointed at ip instead (see dialThroughIP).
func kubeconfigDialingThroughIP(raw []byte, hostname, ip string) ([]byte, error) {
	kubeconfig, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("failed parsing kubeconfig: %w", err)
	}

	if err := dialThroughIP(kubeconfig, hostname, ip); err != nil {
		return nil, err
	}

	out, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed serializing kubeconfig: %w", err)
	}
	return out, nil
}

// dialThroughIP points every cluster whose server host is hostname at ip,
// keeping hostname as the TLS server name so the apiserver certificate still
// verifies.
func dialThroughIP(kubeconfig *clientcmdapi.Config, hostname, ip string) error {
	for name, cluster := range kubeconfig.Clusters {
		server, err := url.Parse(cluster.Server)
		if err != nil {
			return fmt.Errorf("failed parsing API server URL of cluster %q: %w", name, err)
		}
		if server.Hostname() != hostname {
			continue
		}

		server.Host = hostPort(ip, server.Port())
		cluster.Server = server.String()
		cluster.TLSServerName = hostname
	}
	return nil
}

// dialByName points every cluster that carries a TLS server name back at
// that name and drops the TLS server name. Reports whether anything changed.
func dialByName(kubeconfig *clientcmdapi.Config) (bool, error) {
	changed := false
	for name, cluster := range kubeconfig.Clusters {
		if cluster.TLSServerName == "" {
			continue
		}

		server, err := url.Parse(cluster.Server)
		if err != nil {
			return false, fmt.Errorf("failed parsing API server URL of cluster %q: %w", name, err)
		}

		server.Host = hostPort(cluster.TLSServerName, server.Port())
		cluster.Server = server.String()
		cluster.TLSServerName = ""
		changed = true
	}
	return changed, nil
}

// hostPort joins host and port for a URL, bracketing IPv6 hosts. An empty
// port leaves the host alone.
func hostPort(host, port string) string {
	if port != "" {
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}
