// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	testAPIHostname = "api.vpn.example.com"
	testBootstrapIP = "203.0.113.10"
)

func TestDialThroughIP(t *testing.T) {
	tests := []struct {
		name              string
		server            string
		wantServer        string
		wantTLSServerName string
		wantErr           bool
	}{
		{
			name:              "hostname dials the IP and keeps the hostname for TLS",
			server:            "https://" + testAPIHostname + ":6443",
			wantServer:        "https://" + testBootstrapIP + ":6443",
			wantTLSServerName: testAPIHostname,
		},
		{
			name:       "other host is left alone",
			server:     "https://10.0.0.5:6443",
			wantServer: "https://10.0.0.5:6443",
		},
		{
			name:    "invalid server URL fails",
			server:  "://bad",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kubeconfig := &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{"main": {Server: tc.server}},
			}

			err := dialThroughIP(kubeconfig, testAPIHostname, testBootstrapIP)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantServer, kubeconfig.Clusters["main"].Server)
			assert.Equal(t, tc.wantTLSServerName, kubeconfig.Clusters["main"].TLSServerName)
		})
	}
}

func TestDialByName(t *testing.T) {
	tests := []struct {
		name        string
		cluster     clientcmdapi.Cluster
		wantServer  string
		wantChanged bool
	}{
		{
			name: "TLS server name becomes the server host again",
			cluster: clientcmdapi.Cluster{
				Server:        "https://" + testBootstrapIP + ":6443",
				TLSServerName: testAPIHostname,
			},
			wantServer:  "https://" + testAPIHostname + ":6443",
			wantChanged: true,
		},
		{
			name:       "no TLS server name is a no-op",
			cluster:    clientcmdapi.Cluster{Server: "https://" + testAPIHostname + ":6443"},
			wantServer: "https://" + testAPIHostname + ":6443",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := tc.cluster
			kubeconfig := &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{"main": &cluster},
			}

			changed, err := dialByName(kubeconfig)
			require.NoError(t, err)
			assert.Equal(t, tc.wantChanged, changed)
			assert.Equal(t, tc.wantServer, cluster.Server)
			assert.Empty(t, cluster.TLSServerName)
		})
	}
}

func TestHostPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "hostname with port", host: testAPIHostname, port: "6443", want: testAPIHostname + ":6443"},
		{name: "IPv6 with port", host: "2001:db8::1", port: "6443", want: "[2001:db8::1]:6443"},
		{name: "IPv6 without port", host: "2001:db8::1", want: "[2001:db8::1]"},
		{name: "hostname without port", host: testAPIHostname, want: testAPIHostname},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hostPort(tc.host, tc.port))
		})
	}
}

// Mutates outputPathMainClusterKubeconfig — sequential only.
func TestPointMainClusterKubeconfigAtHostname(t *testing.T) {
	t.Run("restores the hostname after dialing through the IP", func(t *testing.T) {
		raw, err := clientcmd.Write(clientcmdapi.Config{
			Clusters: map[string]*clientcmdapi.Cluster{
				testClusterName: {Server: "https://" + testAPIHostname + ":6443"},
			},
			AuthInfos:      map[string]*clientcmdapi.AuthInfo{"admin": {Token: "token"}},
			Contexts:       map[string]*clientcmdapi.Context{"admin": {Cluster: testClusterName, AuthInfo: "admin"}},
			CurrentContext: "admin",
		})
		require.NoError(t, err)

		dialed, err := kubeconfigDialingThroughIP(raw, testAPIHostname, testBootstrapIP)
		require.NoError(t, err)

		dialedConfig, err := clientcmd.Load(dialed)
		require.NoError(t, err)
		assert.Equal(t, "https://"+testBootstrapIP+":6443", dialedConfig.Clusters[testClusterName].Server)
		assert.Equal(t, testAPIHostname, dialedConfig.Clusters[testClusterName].TLSServerName)

		path := filepath.Join(t.TempDir(), "main.yaml")
		require.NoError(t, os.WriteFile(path, dialed, 0o600))

		origPath := outputPathMainClusterKubeconfig
		t.Cleanup(func() { outputPathMainClusterKubeconfig = origPath })
		outputPathMainClusterKubeconfig = path

		require.NoError(t, PointMainClusterKubeconfigAtHostname(context.Background()))

		restored, err := clientcmd.LoadFromFile(path)
		require.NoError(t, err)
		assert.Equal(t, "https://"+testAPIHostname+":6443", restored.Clusters[testClusterName].Server)
		assert.Empty(t, restored.Clusters[testClusterName].TLSServerName)
		assert.Equal(t, "admin", restored.CurrentContext)
	})

	t.Run("missing kubeconfig is a no-op", func(t *testing.T) {
		origPath := outputPathMainClusterKubeconfig
		t.Cleanup(func() { outputPathMainClusterKubeconfig = origPath })
		outputPathMainClusterKubeconfig = filepath.Join(t.TempDir(), "missing.yaml")

		require.NoError(t, PointMainClusterKubeconfigAtHostname(context.Background()))
	})
}
