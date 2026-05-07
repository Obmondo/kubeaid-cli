// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package k3d

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	k3dTypes "github.com/k3d-io/k3d/v5/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func newFakeReleasesServer(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rel := GitHubRelease{TagName: tagName}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func overrideK3sReleasesURL(t *testing.T, url string) {
	t.Helper()
	original := k3sReleasesURL
	k3sReleasesURL = url
	t.Cleanup(func() { k3sReleasesURL = original })
}

func readFileContent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading generated config file")
	return string(data)
}

type fakeK3DRuntime struct {
	clusters         []*k3dTypes.Cluster
	clusterListErr   error
	clusterCreateErr error
	clusterDeleteErr error

	createCalled     bool
	deleteCalled     bool
	createConfigPath string
	deleteConfigPath string

	writeKubeconfigCalled bool
	writeKubeconfigPath   string
	writeKubeconfigErr    error
}

func (f *fakeK3DRuntime) ClusterList(_ context.Context) ([]*k3dTypes.Cluster, error) {
	return f.clusters, f.clusterListErr
}

func (f *fakeK3DRuntime) ClusterCreate(configPath string) error {
	f.createCalled = true
	f.createConfigPath = configPath
	return f.clusterCreateErr
}

func (f *fakeK3DRuntime) ClusterDelete(configPath string) error {
	f.deleteCalled = true
	f.deleteConfigPath = configPath
	return f.clusterDeleteErr
}

func (f *fakeK3DRuntime) WriteKubeconfig(_ context.Context, clusterName string, outputPath string) error {
	f.writeKubeconfigCalled = true
	f.writeKubeconfigPath = outputPath
	if f.writeKubeconfigErr != nil {
		return f.writeKubeconfigErr
	}
	// Write a minimal valid kubeconfig so downstream code can load it.
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://0.0.0.0:12345
  name: k3d-%s
contexts:
- context:
    cluster: k3d-%s
    user: admin
  name: k3d-%s
current-context: k3d-%s
users:
- name: admin
  user:
    token: fake-token
`, clusterName, clusterName, clusterName, clusterName)
	return os.WriteFile(outputPath, []byte(content), 0o600)
}

func TestCreateK3DClusterWithParams(t *testing.T) {
	// Mutates globals — sequential only.

	tests := []struct {
		name               string
		cloudProvider      string
		clusters           []*k3dTypes.Cluster
		clusterListErr     error
		clusterCreateErr   error
		writeKubeconfigErr error
		configPathOverride string
		wantCreateCalled   bool
		wantErr            bool
		wantInConfig       []string
	}{
		{
			name:             "cluster list empty — creates cluster and renders config",
			clusters:         nil,
			wantCreateCalled: true,
			wantInConfig: []string{
				"name: test-cluster",
				"network: k3d-test-cluster",
				"rancher/k3s:v1.30.0-k3s1",
			},
		},
		{
			name: "cluster already exists — skips creation",
			clusters: []*k3dTypes.Cluster{
				{Name: "test-cluster"},
			},
			wantCreateCalled: false,
		},
		{
			name: "cluster exists among multiple — skips creation",
			clusters: []*k3dTypes.Cluster{
				{Name: "other-1"},
				{Name: "test-cluster"},
				{Name: "other-2"},
			},
			wantCreateCalled: false,
		},
		{
			name: "no matching cluster name — creates cluster",
			clusters: []*k3dTypes.Cluster{
				{Name: "other-cluster"},
			},
			wantCreateCalled: true,
		},
		{
			name:           "returns error when ClusterList fails",
			clusterListErr: fmt.Errorf("docker daemon not running"),
			wantErr:        true,
		},
		{
			name:             "returns error when ClusterCreate fails",
			clusters:         nil,
			clusterCreateErr: fmt.Errorf("port already in use"),
			wantCreateCalled: true,
			wantErr:          true,
		},
		{
			name:               "returns error when WriteKubeconfig fails",
			clusters:           nil,
			writeKubeconfigErr: fmt.Errorf("disk full"),
			wantCreateCalled:   true,
			wantErr:            true,
		},
		{
			name:          "returns error when getK3sVersion fails",
			cloudProvider: constants.CloudProviderAWS,
			wantErr:       true,
		},
		{
			name:               "returns error when config path is invalid",
			configPathOverride: "/nonexistent/deeply/nested/dir/k3d.yaml",
			wantErr:            true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origProvider := globals.CloudProviderName
			origConfig := *config.ParsedGeneralConfig
			t.Cleanup(func() {
				globals.CloudProviderName = origProvider
				*config.ParsedGeneralConfig = origConfig
			})

			if tc.cloudProvider != "" {
				globals.CloudProviderName = tc.cloudProvider
				overrideK3sReleasesURL(t, "http://127.0.0.1:0")
			} else {
				globals.CloudProviderName = constants.CloudProviderLocal
				config.ParsedGeneralConfig.Cluster.K8sVersion = "v1.30.0"
			}

			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "k3d-config.yaml")
			if tc.configPathOverride != "" {
				configPath = tc.configPathOverride
			}
			hostKubeconfig := filepath.Join(tmpDir, "host", "kubeconfig")
			containerKubeconfig := filepath.Join(tmpDir, "container", "kubeconfig")

			rt := &fakeK3DRuntime{
				clusters:           tc.clusters,
				clusterListErr:     tc.clusterListErr,
				clusterCreateErr:   tc.clusterCreateErr,
				writeKubeconfigErr: tc.writeKubeconfigErr,
			}

			// Override fixControlPlaneNodeLabelsFn to avoid needing a real cluster.
			origFixFn := fixControlPlaneNodeLabelsFn
			fixControlPlaneNodeLabelsFn = func(_ context.Context, _ string) error { return nil }
			t.Cleanup(func() { fixControlPlaneNodeLabelsFn = origFixFn })

			params := &createK3DClusterParams{
				Runtime:                 rt,
				ConfigPath:              configPath,
				HostKubeconfigPath:      hostKubeconfig,
				ContainerKubeconfigPath: containerKubeconfig,
			}

			err := createK3DClusterWithParams(context.Background(), "test-cluster", params)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tc.wantCreateCalled, rt.createCalled)
			require.True(t, rt.writeKubeconfigCalled)
			assert.Equal(t, hostKubeconfig, rt.writeKubeconfigPath)

			if len(tc.wantInConfig) > 0 {
				content := readFileContent(t, configPath)
				for _, want := range tc.wantInConfig {
					assert.Contains(t, content, want)
				}
			}
		})
	}
}

func TestCreateK3DCluster(t *testing.T) {
	// Mutates package globals and shared config — sequential only.
	origRuntime := DockerRuntime
	origFixFn := fixControlPlaneNodeLabelsFn
	origProvider := globals.CloudProviderName
	origConfig := *config.ParsedGeneralConfig
	origK3DConfigPath := constants.OutputPathManagementClusterK3DConfig
	origHostKubeconfigPath := constants.OutputPathManagementClusterHostKubeconfig
	origContainerKubeconfigPath := constants.OutputPathManagementClusterContainerKubeconfig
	t.Cleanup(func() {
		DockerRuntime = origRuntime
		fixControlPlaneNodeLabelsFn = origFixFn
		globals.CloudProviderName = origProvider
		*config.ParsedGeneralConfig = origConfig
		constants.OutputPathManagementClusterK3DConfig = origK3DConfigPath
		constants.OutputPathManagementClusterHostKubeconfig = origHostKubeconfigPath
		constants.OutputPathManagementClusterContainerKubeconfig = origContainerKubeconfigPath
	})

	tmpDir := t.TempDir()
	constants.OutputPathManagementClusterK3DConfig = filepath.Join(tmpDir, "k3d.config.yaml")
	constants.OutputPathManagementClusterHostKubeconfig = filepath.Join(tmpDir, "kubeconfigs", "host.yaml")
	constants.OutputPathManagementClusterContainerKubeconfig = filepath.Join(tmpDir, "kubeconfigs", "container.yaml")

	rt := &fakeK3DRuntime{}
	DockerRuntime = rt
	fixControlPlaneNodeLabelsFn = func(_ context.Context, _ string) error { return nil }
	globals.CloudProviderName = constants.CloudProviderLocal
	config.ParsedGeneralConfig.Cluster.K8sVersion = "v1.30.0"

	err := CreateK3DCluster(context.Background(), "test-cluster")
	require.NoError(t, err)

	require.True(t, rt.createCalled)
	assert.Equal(t, constants.OutputPathManagementClusterK3DConfig, rt.createConfigPath)
	assert.Equal(t, constants.OutputPathManagementClusterHostKubeconfig, rt.writeKubeconfigPath)
	assert.Contains(t, readFileContent(t, constants.OutputPathManagementClusterK3DConfig), "name: test-cluster")
	assert.Contains(
		t,
		readFileContent(t, constants.OutputPathManagementClusterContainerKubeconfig),
		"server: https://k3d-test-cluster-server-0:6443",
	)
}

func TestDeleteK3DClusterWithRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		configPath       string
		clusterDeleteErr error
		wantErr          bool
	}{
		{
			name:       "calls ClusterDelete with the provided config path",
			configPath: "/tmp/k3d-config.yaml",
		},
		{
			name:       "calls ClusterDelete even when config path is in a nested dir",
			configPath: "/some/nested/dir/k3d-config.yaml",
		},
		{
			name:             "returns error when ClusterDelete fails",
			configPath:       "/tmp/k3d-config.yaml",
			clusterDeleteErr: fmt.Errorf("cluster not found"),
			wantErr:          true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rt := &fakeK3DRuntime{clusterDeleteErr: tc.clusterDeleteErr}
			err := deleteK3DClusterWithRuntime(context.Background(), tc.configPath, rt)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.True(t, rt.deleteCalled, "ClusterDelete should have been called")
			assert.Equal(t, tc.configPath, rt.deleteConfigPath)
		})
	}
}

func TestDeleteK3DCluster(t *testing.T) {
	// Mutates package globals — sequential only.
	origRuntime := DockerRuntime
	origK3DConfigPath := constants.OutputPathManagementClusterK3DConfig
	t.Cleanup(func() {
		DockerRuntime = origRuntime
		constants.OutputPathManagementClusterK3DConfig = origK3DConfigPath
	})

	configPath := filepath.Join(t.TempDir(), "k3d.config.yaml")
	constants.OutputPathManagementClusterK3DConfig = configPath

	rt := &fakeK3DRuntime{}
	DockerRuntime = rt

	err := DeleteK3DCluster(context.Background())
	require.NoError(t, err)

	require.True(t, rt.deleteCalled)
	assert.Equal(t, configPath, rt.deleteConfigPath)
}

func TestGetK3sVersion(t *testing.T) {
	// Mutates globals, config, and k3sReleasesURL — sequential only.

	tests := []struct {
		name          string
		cloudProvider string
		k8sVer        string
		tagName       string
		setupServer   func(t *testing.T) string
		want          string
		wantErr       bool
	}{
		{
			name:          "local provider appends -k3s1 to config K8sVersion",
			cloudProvider: constants.CloudProviderLocal,
			k8sVer:        "v1.29.4",
			want:          "v1.29.4-k3s1",
		},
		{
			name:          "local provider with different K8s version",
			cloudProvider: constants.CloudProviderLocal,
			k8sVer:        "v1.30.0",
			want:          "v1.30.0-k3s1",
		},
		{
			name:          "AWS provider fetches latest K3s version",
			cloudProvider: constants.CloudProviderAWS,
			tagName:       "v1.29.4+k3s1",
			want:          "v1.29.4-k3s1",
		},
		{
			name:          "Hetzner provider fetches latest K3s version",
			cloudProvider: constants.CloudProviderHetzner,
			tagName:       "v1.30.0+k3s2",
			want:          "v1.30.0-k3s2",
		},
		{
			name:          "plus is replaced with hyphen in latest version",
			cloudProvider: constants.CloudProviderAWS,
			tagName:       "v1.29.4+k3s1",
			want:          "v1.29.4-k3s1",
		},
		{
			name:          "no plus leaves tag unchanged",
			cloudProvider: constants.CloudProviderAWS,
			tagName:       "v1.29.4-k3s1",
			want:          "v1.29.4-k3s1",
		},
		{
			name:          "multiple plus signs are all replaced",
			cloudProvider: constants.CloudProviderAWS,
			tagName:       "v1.29.4+k3s1+extra",
			want:          "v1.29.4-k3s1-extra",
		},
		{
			name:          "empty tagName returns empty string",
			cloudProvider: constants.CloudProviderAWS,
			tagName:       "",
			want:          "",
		},
		{
			name:          "returns error when server returns bad JSON",
			cloudProvider: constants.CloudProviderAWS,
			setupServer: func(t *testing.T) string {
				t.Helper()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("not valid json"))
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantErr: true,
		},
		{
			name:          "returns error when server returns non-200 status",
			cloudProvider: constants.CloudProviderAWS,
			setupServer: func(t *testing.T) string {
				t.Helper()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantErr: true,
		},
		{
			name:          "returns error when server is unreachable",
			cloudProvider: constants.CloudProviderAWS,
			setupServer: func(t *testing.T) string {
				t.Helper()
				return "http://127.0.0.1:0"
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origProvider := globals.CloudProviderName
			origK8sVer := config.ParsedGeneralConfig.Cluster.K8sVersion
			t.Cleanup(func() {
				globals.CloudProviderName = origProvider
				config.ParsedGeneralConfig.Cluster.K8sVersion = origK8sVer
			})

			globals.CloudProviderName = tc.cloudProvider
			config.ParsedGeneralConfig.Cluster.K8sVersion = tc.k8sVer

			if tc.setupServer != nil {
				overrideK3sReleasesURL(t, tc.setupServer(t))
			} else if tc.cloudProvider != constants.CloudProviderLocal {
				srv := newFakeReleasesServer(t, tc.tagName)
				overrideK3sReleasesURL(t, srv.URL)
			}

			got, err := getK3sVersion()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetMaxK3sSupportedK8sVersion(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) string
		wantK8sVer  string
		wantErr     bool
	}{
		{
			name: "extracts version before dash from standard tag",
			setupServer: func(t *testing.T) string {
				t.Helper()
				return newFakeReleasesServer(t, "v1.29.4+k3s1").URL
			},
			wantK8sVer: "v1.29.4",
		},
		{
			name: "works with double-digit patch version",
			setupServer: func(t *testing.T) string {
				t.Helper()
				return newFakeReleasesServer(t, "v1.28.12+k3s1").URL
			},
			wantK8sVer: "v1.28.12",
		},
		{
			name: "works when already hyphenated (no plus in source)",
			setupServer: func(t *testing.T) string {
				t.Helper()
				return newFakeReleasesServer(t, "v1.30.0-k3s2").URL
			},
			wantK8sVer: "v1.30.0",
		},
		{
			name: "returns error when version has no dash separator",
			setupServer: func(t *testing.T) string {
				t.Helper()
				return newFakeReleasesServer(t, "v1.29.4").URL
			},
			wantErr: true,
		},
		{
			name: "returns error when getLatestK3sVersion fails",
			setupServer: func(t *testing.T) string {
				t.Helper()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			overrideK3sReleasesURL(t, tc.setupServer(t))

			got, err := GetMaxK3sSupportedK8sVersion(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantK8sVer, got)
		})
	}
}
