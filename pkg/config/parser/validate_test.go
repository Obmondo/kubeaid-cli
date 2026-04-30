// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

func stubK8sVersionDeps(t *testing.T) {
	t.Helper()
	resetNowFn(t)
	resetLifecyclesFn(t)
	resetLatestStableK8sReleaseFn(t)

	frozen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	farFutureEOL := "2099-12-31"

	nowFn = func() time.Time { return frozen }
	lifecyclesFn = func() (map[string]k8sLifecycle, error) {
		entries := map[string]k8sLifecycle{}
		for major := 1; major <= 1; major++ {
			for minor := 30; minor <= 40; minor++ {
				cycle := stubCycle(major, minor)
				entries[cycle] = k8sLifecycle{Cycle: cycle, EOL: farFutureEOL}
			}
		}
		return entries, nil
	}
	latestStableK8sReleaseFn = func() (string, error) {
		return "v9.99.99", nil
	}
}

func stubCycle(major, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
}

func withParsedConfig(
	t *testing.T, general *config.GeneralConfig, secrets *config.SecretsConfig,
) {
	t.Helper()
	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origGeneral
		config.ParsedSecretsConfig = origSecrets
	})
	config.ParsedGeneralConfig = general
	config.ParsedSecretsConfig = secrets
}

func minimalLocalConfig(clusterName string) *config.GeneralConfig {
	return &config.GeneralConfig{
		Git: config.GitConfig{
			SSHUsername: "git",
		},
		Forks: config.ForksConfig{
			KubeaidFork: config.KubeAidForkConfig{
				URL:     "git@github.com:example/kubeaid.git",
				Version: "v1.0.0",
			},
			KubeaidConfigFork: config.KubeaidConfigForkConfig{
				URL: "git@github.com:example/kubeaid-config.git",
			},
		},
		Cluster: config.ClusterConfig{
			Name:       clusterName,
			Type:       "workload",
			K8sVersion: "v1.31.0",
			ArgoCD: config.ArgoCDConfig{
				DeployKeys: config.DeployKeysConfig{
					Kubeaid: &config.SSHKeyPairConfig{
						PrivateKeyFilePath: "kubeaid-key",
					},
					KubeaidConfig: config.SSHKeyPairConfig{
						PrivateKeyFilePath: "kubeaid-config-key",
					},
				},
			},
		},
		Cloud: config.CloudConfig{
			Local: &config.LocalConfig{},
		},
	}
}

func TestValidateConfigs(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
	}{
		{name: "single embedded dot", clusterName: "kube.cluster.name"},
		{name: "leading dot", clusterName: ".cluster"},
		{name: "trailing dot", clusterName: "cluster."},
		{name: "only a dot", clusterName: "."},
		{name: "subdomain style", clusterName: "prod.eu.k8s"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalGeneral := config.ParsedGeneralConfig
			originalSecrets := config.ParsedSecretsConfig
			t.Cleanup(func() {
				config.ParsedGeneralConfig = originalGeneral
				config.ParsedSecretsConfig = originalSecrets
			})

			config.ParsedGeneralConfig = minimalLocalConfig(tc.clusterName)
			config.ParsedSecretsConfig = &config.SecretsConfig{}

			err := validateConfigs(context.Background())
			require.Error(t, err)
			assert.EqualError(t, err, "cluster name cannot contain any dots")
		})
	}
}

func TestValidateConfigsHetznerControlPlaneLoadBalancerHostname(t *testing.T) {
	stubK8sVersionDeps(t)

	tests := []struct {
		name       string
		hostname   string
		wantErr    bool
		wantErrSub string
	}{
		{
			name:     "empty hostname passes",
			hostname: "",
		},
		{
			name:     "fqdn hostname passes",
			hostname: "api.example.com",
		},
		{
			name:       "short hostname is rejected",
			hostname:   "api",
			wantErr:    true,
			wantErrSub: "Hostname",
		},
		{
			name:       "hostname with scheme is rejected",
			hostname:   "https://api.example.com",
			wantErr:    true,
			wantErrSub: "Hostname",
		},
		{
			name:       "hostname with port is rejected",
			hostname:   "api.example.com:6443",
			wantErr:    true,
			wantErrSub: "Hostname",
		},
		{
			name:       "hostname with whitespace is rejected",
			hostname:   "api example.com",
			wantErr:    true,
			wantErrSub: "Hostname",
		},
		{
			name:       "ip address is rejected",
			hostname:   "1.2.3.4",
			wantErr:    true,
			wantErrSub: "must be a DNS name, not an IP address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			general := minimalLocalConfig("demo")
			general.ImagePullPolicy = "IfNotPresent"
			general.Cloud = config.CloudConfig{
				Hetzner: &config.HetznerConfig{
					Mode: constants.HetznerModeHCloud,
					SSHKeyPair: config.HetznerSSHKeyPair{
						Name: "demo",
						SSHKeyPairConfig: config.SSHKeyPairConfig{
							PrivateKeyFilePath: "hetzner-key",
						},
					},
					HCloud: &config.HCloudConfig{
						Zone:      "eu-central",
						ImageName: "ubuntu-24.04",
						HetznerNetwork: config.HetznerNetworkConfig{
							CIDR:                    "10.0.0.0/16",
							HCloudServersSubnetCIDR: "10.0.0.0/24",
						},
					},
					ControlPlane: config.HetznerControlPlane{
						Regions: []string{"hel1"},
						HCloud: &config.HCloudControlPlane{
							MachineType: "cax11",
							Replicas:    1,
							LoadBalancer: config.HCloudControlPlaneLoadBalancer{
								Enabled:  true,
								Region:   "hel1",
								Hostname: tc.hostname,
							},
						},
					},
				},
			}
			secrets := &config.SecretsConfig{
				Hetzner: &config.HetznerCredentials{
					APIToken: "token",
				},
			}
			withParsedConfig(t, general, secrets)

			origProvider := globals.CloudProviderName
			t.Cleanup(func() { globals.CloudProviderName = origProvider })
			globals.CloudProviderName = constants.CloudProviderHetzner

			err := validateConfigs(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateLabelsAndTaints(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		wantErr    bool
		wantErrSub string
	}{
		{
			name:   "no labels",
			labels: map[string]string{},
		},
		{
			name: "node-role label",
			labels: map[string]string{
				"node-role.kubernetes.io/worker": "",
			},
		},
		{
			name: "node-restriction label",
			labels: map[string]string{
				"node-restriction.kubernetes.io/region": "eu-west",
			},
		},
		{
			name: "cluster.x-k8s.io label",
			labels: map[string]string{
				"node.cluster.x-k8s.io/role": "worker",
			},
		},
		{
			name: "label outside allowed domains is rejected",
			labels: map[string]string{
				"example.com/team": "platform",
			},
			wantErr:    true,
			wantErrSub: "should belong to one of these domains",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLabelsAndTaints("ng-test", tc.labels, nil)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateKnownHostsEntries(t *testing.T) {
	validKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHZLLpBn+ig1bdyf+9SLB0wbIMcfaNs+M+Co7ZW0ykzl"
	validPlainHost := "gitea.example.com " + validKey
	validPortHost := "[gitea.example.com]:2223 " + validKey

	tests := []struct {
		name       string
		entries    []string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "nil slice passes",
			entries: nil,
		},
		{
			name:    "empty slice passes",
			entries: []string{},
		},
		{
			name:    "single valid hostname entry passes",
			entries: []string{validPlainHost},
		},
		{
			name:    "valid entry with bracketed host:port passes",
			entries: []string{validPortHost},
		},
		{
			name:    "mix of valid entries passes",
			entries: []string{validPlainHost, validPortHost},
		},
		{
			name:       "empty entry is rejected",
			entries:    []string{""},
			wantErr:    true,
			wantErrMsg: "entry 0 is empty",
		},
		{
			name:       "whitespace-only entry is rejected",
			entries:    []string{"   \t  "},
			wantErr:    true,
			wantErrMsg: "entry 0 is empty",
		},
		{
			name:       "malformed entry is rejected",
			entries:    []string{"this is not a known_hosts line"},
			wantErr:    true,
			wantErrMsg: "entry 0",
		},
		{
			name:       "multi-line entry is rejected",
			entries:    []string{validPlainHost + "\n" + validPortHost},
			wantErr:    true,
			wantErrMsg: "contains multiple lines",
		},
		{
			name:       "second entry invalid reports correct index",
			entries:    []string{validPlainHost, "garbage"},
			wantErr:    true,
			wantErrMsg: "entry 1",
		},
		{
			name: "multiple distinct valid entries pass",
			entries: []string{
				validPlainHost,
				validPortHost,
				"gitlab.example.com " + validKey,
			},
		},
		{
			name:       "comment-only line is rejected",
			entries:    []string{"# this is just a comment"},
			wantErr:    true,
			wantErrMsg: "entry 0",
		},
		{
			name:    "valid entry with surrounding whitespace passes",
			entries: []string{"\t  " + validPlainHost + "  "},
		},
		{
			name:       "entry with carriage-return + newline rejected as multi-line",
			entries:    []string{validPlainHost + "\r\n" + validPortHost},
			wantErr:    true,
			wantErrMsg: "contains multiple lines",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateKnownHostsEntries(context.Background(), tc.entries)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateK8sVersion(t *testing.T) {
	tests := []struct {
		name       string
		k8sVersion string
		wantErr    bool
		wantErrSub string
	}{
		{
			name:       "min supported (KubeOne) passes",
			k8sVersion: constants.MinKubeOneSupportedK8sVersion + ".0",
		},
		{
			name:       "max supported (KubeOne) passes",
			k8sVersion: constants.MaxKubeOneSupportedK8sVersion + ".0",
		},
		{
			name:       "non-zero patch within max minor passes",
			k8sVersion: constants.MaxKubeOneSupportedK8sVersion + ".5",
		},
		{
			name:       "missing v prefix is rejected",
			k8sVersion: "1.31.0",
			wantErr:    true,
			wantErrSub: "K8s version must start with 'v'",
		},
		{
			name:       "malformed version is rejected",
			k8sVersion: "vbroken",
			wantErr:    true,
			wantErrSub: "parsing K8s semantic version",
		},
		{
			name:       "above max bare-metal is rejected",
			k8sVersion: "v1.99.0",
			wantErr:    true,
			wantErrSub: "K8s version must be in the range",
		},
		{
			name:       "below min bare-metal is rejected",
			k8sVersion: "v1.0.0",
			wantErr:    true,
			wantErrSub: "K8s version must be in the range",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalProvider := globals.CloudProviderName
			t.Cleanup(func() { globals.CloudProviderName = originalProvider })
			globals.CloudProviderName = constants.CloudProviderBareMetal
			stubK8sVersionDeps(t)

			err := validateK8sVersion(context.Background(), tc.k8sVersion)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func runCloudCredentialsValidator(
	t *testing.T,
	cases []struct {
		name       string
		secrets    *config.SecretsConfig
		general    *config.GeneralConfig
		wantErr    bool
		wantErrSub string
	},
	validate func() error,
) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withParsedConfig(t, tc.general, tc.secrets)

			err := validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateAWSConfig(t *testing.T) {
	runCloudCredentialsValidator(t, []struct {
		name       string
		secrets    *config.SecretsConfig
		general    *config.GeneralConfig
		wantErr    bool
		wantErrSub string
	}{
		{
			name:       "missing AWS credentials is rejected",
			secrets:    &config.SecretsConfig{},
			general:    &config.GeneralConfig{Cloud: config.CloudConfig{AWS: &config.AWSConfig{}}},
			wantErr:    true,
			wantErrSub: "AWS credentials not provided",
		},
		{
			name:    "AWS credentials with no node-groups passes",
			secrets: &config.SecretsConfig{AWS: &config.AWSCredentials{}},
			general: &config.GeneralConfig{Cloud: config.CloudConfig{AWS: &config.AWSConfig{}}},
		},
	}, validateAWSConfig)
}

func TestValidateAzureConfig(t *testing.T) {
	runCloudCredentialsValidator(t, []struct {
		name       string
		secrets    *config.SecretsConfig
		general    *config.GeneralConfig
		wantErr    bool
		wantErrSub string
	}{
		{
			name:       "missing Azure credentials is rejected",
			secrets:    &config.SecretsConfig{},
			general:    &config.GeneralConfig{Cloud: config.CloudConfig{Azure: &config.AzureConfig{}}},
			wantErr:    true,
			wantErrSub: "azure credentials not provided",
		},
		{
			name:    "Azure credentials with no node-groups passes",
			secrets: &config.SecretsConfig{Azure: &config.AzureCredentials{}},
			general: &config.GeneralConfig{Cloud: config.CloudConfig{Azure: &config.AzureConfig{}}},
		},
	}, validateAzureConfig)
}

func TestValidateHetznerConfig(t *testing.T) {
	tests := []struct {
		name       string
		secrets    *config.SecretsConfig
		general    *config.GeneralConfig
		wantErr    bool
		wantErrSub string
	}{
		{
			name:    "missing Hetzner credentials is rejected",
			secrets: &config.SecretsConfig{},
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{Mode: constants.HetznerModeHCloud},
				},
			},
			wantErr:    true,
			wantErrSub: "hetzner credentials not provided",
		},
		{
			name:    "VPN cluster type with non-hcloud mode is rejected",
			secrets: &config.SecretsConfig{Hetzner: &config.HetznerCredentials{}},
			general: &config.GeneralConfig{
				Cluster: config.ClusterConfig{Type: constants.ClusterTypeVPN},
				Cloud: config.CloudConfig{Hetzner: &config.HetznerConfig{
					Mode:             constants.HetznerModeBareMetal,
					HCloudVPNCluster: &config.HCloudVPNClusterConfig{Name: "vpn"},
				}},
			},
			wantErr:    true,
			wantErrSub: "VPN cluster can only exist in HCloud",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withParsedConfig(t, tc.general, tc.secrets)

			err := validateHetznerConfig()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateHCloudConfig(t *testing.T) {
	tests := []struct {
		name       string
		general    *config.GeneralConfig
		wantErr    bool
		wantErrSub string
	}{
		{
			name: "missing HCloud details is rejected",
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{Mode: constants.HetznerModeHCloud},
				},
			},
			wantErr:    true,
			wantErrSub: "HCloud specific details not provided",
		},
		{
			name: "control-plane in HCloud but no HCloud control-plane details is rejected",
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						Mode:   constants.HetznerModeHCloud,
						HCloud: &config.HCloudConfig{},
					},
				},
			},
			wantErr:    true,
			wantErrSub: "HCloud specific control-plane details not provided",
		},
		{
			name: "HCloud + control-plane provided passes",
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						Mode:         constants.HetznerModeHCloud,
						HCloud:       &config.HCloudConfig{},
						ControlPlane: config.HetznerControlPlane{HCloud: &config.HCloudControlPlane{}},
					},
				},
			},
		},
		{
			name: "HCloud control-plane load-balancer hostname cannot be an IP address",
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						Mode:   constants.HetznerModeHCloud,
						HCloud: &config.HCloudConfig{},
						ControlPlane: config.HetznerControlPlane{
							HCloud: &config.HCloudControlPlane{
								LoadBalancer: config.HCloudControlPlaneLoadBalancer{
									Hostname: "1.2.3.4",
								},
							},
						},
					},
				},
			},
			wantErr:    true,
			wantErrSub: "must be a DNS name, not an IP address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origGeneral := config.ParsedGeneralConfig
			t.Cleanup(func() { config.ParsedGeneralConfig = origGeneral })
			config.ParsedGeneralConfig = tc.general

			err := validateHCloudConfig()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateHetznerBareMetalConfig(t *testing.T) {
	tests := []struct {
		name       string
		secrets    *config.SecretsConfig
		general    *config.GeneralConfig
		wantErr    bool
		wantErrSub string
	}{
		{
			name:    "missing robot user/password is rejected",
			secrets: &config.SecretsConfig{Hetzner: &config.HetznerCredentials{}},
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{Mode: constants.HetznerModeBareMetal},
				},
			},
			wantErr:    true,
			wantErrSub: "hetzner robot user and password not provided",
		},
		{
			name: "missing BareMetal section is rejected",
			secrets: &config.SecretsConfig{
				Hetzner: &config.HetznerCredentials{Robot: &config.HetznerRobotCredentials{}},
			},
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{Mode: constants.HetznerModeBareMetal},
				},
			},
			wantErr:    true,
			wantErrSub: "hetzner bare metal specific details not provided",
		},
		{
			name: "hybrid mode without VSwitch is rejected",
			secrets: &config.SecretsConfig{
				Hetzner: &config.HetznerCredentials{Robot: &config.HetznerRobotCredentials{}},
			},
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						Mode:      constants.HetznerModeHybrid,
						BareMetal: &config.HetznerBareMetalConfig{},
					},
				},
			},
			wantErr:    true,
			wantErrSub: "VSwitch details not provided",
		},
		{
			name: "control-plane in bare metal but no bare-metal CP details is rejected",
			secrets: &config.SecretsConfig{
				Hetzner: &config.HetznerCredentials{Robot: &config.HetznerRobotCredentials{}},
			},
			general: &config.GeneralConfig{
				Cloud: config.CloudConfig{
					Hetzner: &config.HetznerConfig{
						Mode:      constants.HetznerModeBareMetal,
						BareMetal: &config.HetznerBareMetalConfig{},
					},
				},
			},
			wantErr:    true,
			wantErrSub: "hetzner bare metal specific control-plane details not provided",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withParsedConfig(t, tc.general, tc.secrets)

			err := validateHetznerBareMetalConfig()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateAutoScalableNodeGroup(t *testing.T) {
	tests := []struct {
		name       string
		ng         *config.AutoScalableNodeGroup
		wantErr    bool
		wantErrSub string
	}{
		{
			name: "min <= max passes",
			ng: &config.AutoScalableNodeGroup{
				NodeGroup: config.NodeGroup{Name: "ng-a"},
				MinSize:   1,
				Maxsize:   3,
			},
		},
		{
			name: "min == max passes",
			ng: &config.AutoScalableNodeGroup{
				NodeGroup: config.NodeGroup{Name: "ng-b"},
				MinSize:   2,
				Maxsize:   2,
			},
		},
		{
			name: "min > max is rejected",
			ng: &config.AutoScalableNodeGroup{
				NodeGroup: config.NodeGroup{Name: "ng-c"},
				MinSize:   5,
				Maxsize:   2,
			},
			wantErr:    true,
			wantErrSub: `node-group "ng-c"`,
		},
		{
			name: "invalid label propagates from validateNodeGroup",
			ng: &config.AutoScalableNodeGroup{
				NodeGroup: config.NodeGroup{
					Name:   "ng-d",
					Labels: map[string]string{"example.com/team": "platform"},
				},
				MinSize: 1,
				Maxsize: 1,
			},
			wantErr:    true,
			wantErrSub: "should belong to one of these domains",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAutoScalableNodeGroup(tc.ng)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateNodeGroup(t *testing.T) {
	tests := []struct {
		name       string
		ng         *config.NodeGroup
		wantErr    bool
		wantErrSub string
	}{
		{
			name: "node-group with no labels/taints passes",
			ng:   &config.NodeGroup{Name: "ng-a"},
		},
		{
			name: "node-group with invalid label is rejected",
			ng: &config.NodeGroup{
				Name:   "ng-b",
				Labels: map[string]string{"example.com/team": "platform"},
			},
			wantErr:    true,
			wantErrSub: "should belong to one of these domains",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNodeGroup(tc.ng)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateObmondoMonitoringConfig(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials)
		wantErr    bool
		wantErrSub string
	}{
		{
			name: "empty CertPath is rejected",
			setup: func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials) {
				return &config.ObmondoConfig{Monitoring: true}, nil
			},
			wantErr:    true,
			wantErrSub: "obmondo.certPath is empty",
		},
		{
			name: "empty KeyPath is rejected",
			setup: func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials) {
				certPath := filepath.Join(t.TempDir(), "cert.pem")
				require.NoError(t, os.WriteFile(certPath, []byte("c"), 0o600))
				return &config.ObmondoConfig{Monitoring: true, CertPath: certPath}, nil
			},
			wantErr:    true,
			wantErrSub: "obmondo.keyPath is empty",
		},
		{
			name: "missing Cert file is rejected",
			setup: func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials) {
				return &config.ObmondoConfig{
					Monitoring: true,
					CertPath:   "/non/existent/cert.pem",
					KeyPath:    "/non/existent/key.pem",
				}, nil
			},
			wantErr:    true,
			wantErrSub: "obmondo.certPath",
		},
		{
			name: "valid Cert + Key but missing teleport token is rejected",
			setup: func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials) {
				dir := t.TempDir()
				cert := filepath.Join(dir, "c.pem")
				key := filepath.Join(dir, "k.pem")
				require.NoError(t, os.WriteFile(cert, []byte("c"), 0o600))
				require.NoError(t, os.WriteFile(key, []byte("k"), 0o600))
				return &config.ObmondoConfig{Monitoring: true, CertPath: cert, KeyPath: key}, nil
			},
			wantErr:    true,
			wantErrSub: "secrets.obmondo.teleportAuthToken is required",
		},
		{
			name: "valid Cert + Key + teleport token passes",
			setup: func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials) {
				dir := t.TempDir()
				cert := filepath.Join(dir, "c.pem")
				key := filepath.Join(dir, "k.pem")
				require.NoError(t, os.WriteFile(cert, []byte("c"), 0o600))
				require.NoError(t, os.WriteFile(key, []byte("k"), 0o600))
				return &config.ObmondoConfig{Monitoring: true, CertPath: cert, KeyPath: key},
					&config.ObmondoCredentials{TeleportAuthToken: "tok"}
			},
		},
		{
			name: "valid Cert + Key with teleportAgent disabled passes without token",
			setup: func(t *testing.T) (*config.ObmondoConfig, *config.ObmondoCredentials) {
				dir := t.TempDir()
				cert := filepath.Join(dir, "c.pem")
				key := filepath.Join(dir, "k.pem")
				require.NoError(t, os.WriteFile(cert, []byte("c"), 0o600))
				require.NoError(t, os.WriteFile(key, []byte("k"), 0o600))
				disabled := false
				return &config.ObmondoConfig{
					Monitoring:    true,
					CertPath:      cert,
					KeyPath:       key,
					TeleportAgent: &disabled,
				}, nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
			t.Cleanup(func() {
				config.ParsedGeneralConfig = origGeneral
				config.ParsedSecretsConfig = origSecrets
			})

			obmondoCfg, obmondoSecrets := tc.setup(t)
			config.ParsedGeneralConfig = &config.GeneralConfig{Obmondo: obmondoCfg}
			config.ParsedSecretsConfig = &config.SecretsConfig{Obmondo: obmondoSecrets}

			err := validateObmondoMonitoringConfig()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
		})
	}
}
