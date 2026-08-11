// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates the golden fixtures in testdata/golden from the
// current Render() output. Run:
//
//	go test ./pkg/config/prompt/... -run TestRenderGoldenParity -update-golden
var updateGolden = flag.Bool("update-golden", false, "write current Render() output as the golden fixtures")

// goldenCase is one representative PromptedConfig for a provider/mode
// combination, used to pin Render's output byte-for-byte.
type goldenCase struct {
	name string
	cfg  *PromptedConfig
}

// goldenCases covers every {{if eq .CloudProvider ...}} and
// {{if eq .HetznerMode ...}} branch in general.yaml.tmpl and
// secrets.yaml.tmpl, plus the vpn/workload split and the optional Obmondo
// block — one representative config per provider, as required by the
// add-cluster design's renderer-parity test.
func goldenCases() []goldenCase {
	hcloudWorkloadLockdown := false

	return []goldenCase{
		{
			name: "aws-workload",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "31.0.4",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
				ClusterName:                "aws-acme",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.6",
				NetBirdDNS:                 "netbird.vpn.acme.com",
				NetBirdDNSZone:             "aws-acme.local",
				NetBirdAPIKey:              "nbp_faketoken",

				CloudProvider:       "aws",
				AWSRegion:           "eu-west-1",
				AWSSSHKeyName:       "aws-acme",
				AWSCPInstanceType:   "t3.medium",
				AWSNodeInstanceType: "c6i.xlarge",
				AWSNodeAMIID:        "ami-0fedcba9876543210",
				AWSCPReplicas:       "3",
				AWSAMIID:            "ami-0123456789abcdef0",
				AWSAccessKeyID:      "AKIAFAKEEXAMPLE",
				AWSSecretAccessKey:  "fake/secret:key#1",
				AWSSessionToken:     "fake-session-token",
			},
		},
		{
			name: "azure-workload",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "31.0.4",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
				ClusterName:                "azure-acme",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.6",
				// Declined the NetBird mesh join (NetBirdDNSZone empty on a
				// workload cluster): exercises NetBirdBlockEnabled() == false,
				// the one CloudProvider branch not already covered by the
				// hybrid/bare-metal cases below.

				CloudProvider:          "azure",
				AzureTenantID:          "11111111-1111-1111-1111-111111111111",
				AzureSubscriptionID:    "22222222-2222-2222-2222-222222222222",
				AzureLocation:          "westeurope",
				AzureStorageAccount:    "azureacmestorage",
				AzureCPVMSize:          "Standard_D2s_v3",
				AzureCPReplicas:        "3",
				AzureCPDiskSizeGB:      "128",
				AzureNodeVMSize:        "Standard_F4s_v2",
				AzureClientID:          "33333333-3333-3333-3333-333333333333",
				AzureClientSecret:      "fake&azure:secret",
				AzureSSHPublicKey:      "ssh-rsa AAAAB3FAKEKEY ops@acme.com",
				AzureOIDCIssuerKeyPath: "/home/ops/.ssh/azure-oidc-issuer",
				AzurePrincipalID:       "44444444-4444-4444-4444-444444444444",
			},
		},
		{
			name: "azure-aks-workload",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "31.0.4",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
				ClusterName:                "azure-aks-acme",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.6",

				CloudProvider: "azure",
				// AKS: managed control plane — no storage account, no
				// controlPlane block in the rendered general.yaml.
				AzureAKS:            true,
				AzureTenantID:       "11111111-1111-1111-1111-111111111111",
				AzureSubscriptionID: "22222222-2222-2222-2222-222222222222",
				AzureLocation:       "westeurope",
				AzureNodeVMSize:     "Standard_F4s_v2",
				AzureClientID:       "33333333-3333-3333-3333-333333333333",
				AzureClientSecret:   "fake&azure:secret",
			},
		},
		{
			name: "hetzner-hcloud-workload",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "31.0.4",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
				ClusterName:                "hcloud-acme",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.6",
				Lockdown:                   &hcloudWorkloadLockdown,
				NetBirdDNS:                 "netbird.vpn.acme.com",
				NetBirdDNSZone:             "hcloud-acme.local",
				NetBirdAPIKey:              "nbp_faketoken2",

				CloudProvider:        "hetzner",
				HetznerMode:          "hcloud",
				HetznerSSHKeyName:    "hcloud-acme",
				HetznerHCloudZone:    "eu-central",
				HetznerCPMachineType: "cax21",
				HetznerCPReplicas:    "3",
				HetznerRegion:        "hel1",
				HetznerLBRegion:      "hel1",
				HetznerAPIToken:      "fake-hcloud-token",
			},
		},
		{
			// Mirrors TestRenderHetznerHybridVPN in render_hybrid_test.go —
			// HCloud control plane, bare-metal worker node group, vSwitch,
			// managed Keycloak. Already proven valid via validator.Struct().
			name: "hetzner-hybrid-vpn",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "29.0.9",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				ClusterName:                "hybrid-acme",
				ClusterType:                "vpn",
				ControlPlaneEndpoint:       "api.hybrid.acme.com",
				ACMEEmail:                  "ops@acme.com",
				NetBirdDNS:                 "netbird.hybrid.acme.com",
				KeycloakMode:               "managed",
				KeycloakDNS:                "keycloak.hybrid.acme.com",
				KeycloakRealm:              "acme",
				K8sVersion:                 "v1.35.4",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",

				CloudProvider: "hetzner",

				HetznerMode:          "hybrid",
				HetznerSSHKeyName:    "demo-hybrid",
				HetznerCPReplicas:    "3",
				HetznerHCloudZone:    "eu-central",
				HetznerCPMachineType: "cax21",
				HetznerRegion:        "hel1",
				HetznerLBRegion:      "hel1",

				HetznerVSwitchName:       "hybrid-acme-vswitch",
				HetznerVSwitchVLANID:     "4001",
				HetznerVSwitchSubnetCIDR: "10.0.1.0/24",

				HetznerBMNodeGroupName:       "workers",
				HetznerBMNodeGroupServerIDs:  []string{"1234570", "1234571"},
				HetznerBMNodeGroupPrivateIPs: []string{"10.0.1.10", "10.0.1.11"},
				HetznerBMServerPublicIPs: map[string]string{
					"1234570": "5.5.5.10",
					"1234571": "5.5.5.11",
				},

				HetznerAPIToken:      "fake-token",
				HetznerRobotUser:     "fake-user",
				HetznerRobotPassword: "fake-pass",
			},
		},
		{
			// Mirrors TestRenderHetznerBareMetalWorkload in
			// render_baremetal_test.go — pure bare-metal control plane +
			// node group, vSwitch, workload lockdown. Already proven valid
			// via validator.Struct().
			name: "hetzner-baremetal-workload",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "29.0.9",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				ClusterName:                "bm-acme",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.4",
				Lockdown:                   boolPtr(true),
				NetBirdDNS:                 "netbird.vpn.acme.com",
				NetBirdDNSZone:             "bm-acme.local",
				NetBirdAPIKey:              "nbp_faketoken",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",

				CloudProvider:     "hetzner",
				HetznerMode:       "bare-metal",
				HetznerSSHKeyName: "bm-acme",
				HetznerCPReplicas: "3",

				HetznerVSwitchName:       "bm-acme-vswitch",
				HetznerVSwitchVLANID:     "4002",
				HetznerVSwitchSubnetCIDR: "10.0.1.0/24",

				HetznerBMCPServerIDs:          []string{"1234567", "1234568", "1234569"},
				HetznerBMCPPrivateIPs:         []string{"10.0.1.1", "10.0.1.2", "10.0.1.3"},
				HetznerBMCPRegions:            []string{"fsn1"},
				HetznerBMNodeGroupName:        "workers",
				HetznerBMNodeGroupServerIDs:   []string{"1234570"},
				HetznerBMNodeGroupPrivateIPs:  []string{"10.0.1.10"},
				HetznerBMEndpointHost:         "1.2.3.4",
				HetznerBMEndpointIsFailoverIP: true,
				HetznerBMServerPublicIPs: map[string]string{
					"1234567": "5.5.5.1",
					"1234568": "5.5.5.2",
					"1234569": "5.5.5.3",
					"1234570": "5.5.5.4",
				},

				HetznerAPIToken:      "fake-token",
				HetznerRobotUser:     "#ws+fake-robot-user",
				HetznerRobotPassword: "fake-pass",
			},
		},
		{
			// Mirrors TestRenderGenericBareMetalWorkload in
			// render_generic_baremetal_test.go — generic (non-Hetzner)
			// bare-metal control plane + worker hosts.
			name: "baremetal-generic-workload",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "31.0.4",
				KubeaidConfigForkURL:       "git@github.com:demo/kubeaid-config.git",
				ClusterName:                "demo",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.6",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",

				CloudProvider:              "bare-metal",
				BareMetalSSHPort:           "22",
				BareMetalEndpointHost:      "192.0.2.10",
				BareMetalEndpointPort:      "6443",
				BareMetalControlPlaneHosts: []string{"192.0.2.10", "192.0.2.11"},
				BareMetalWorkerHosts:       []string{"192.0.2.20"},
				BareMetalNodeGroupName:     "workers",
			},
		},
		{
			name: "local-with-obmondo",
			cfg: &PromptedConfig{
				SSHUsername:                "git",
				UseSSHAgent:                true,
				KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
				KubeaidVersion:             "31.0.4",
				KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
				KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
				ClusterName:                "local-dev",
				ClusterType:                "workload",
				K8sVersion:                 "v1.35.6",

				CloudProvider: "local",

				Obmondo: &ObmondoConfig{
					Monitoring: true,
					CertPath:   "/etc/obmondo/client.crt",
					KeyPath:    "/etc/obmondo/client.key",
				},
			},
		},
	}
}

func boolPtr(b bool) *bool { return &b }

// TestRenderGoldenParity is the add-cluster design's renderer-parity test:
// it pins Render's output against checked-in golden fixtures (one per
// provider/mode, captured from writeConfigFiles before Render was
// extracted from it) and proves writeConfigFiles's on-disk output stays
// byte-identical to what Render returns. That equivalence is what makes
// the Obmondo API's use of Render trustworthy as "the same bytes
// `config generate` would have written" — see docs/add-cluster/design.md.
func TestRenderGoldenParity(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			gotGeneral, gotSecrets, err := Render(tc.cfg)
			require.NoError(t, err)

			generalGoldenPath := filepath.Join("testdata", "golden", tc.name+".general.yaml")
			secretsGoldenPath := filepath.Join("testdata", "golden", tc.name+".secrets.yaml")

			if *updateGolden {
				// 0600 to satisfy gosec G306. Git records only the
				// executable bit, so the committed fixtures are unaffected.
				require.NoError(t, os.WriteFile(generalGoldenPath, gotGeneral, 0o600))
				require.NoError(t, os.WriteFile(secretsGoldenPath, gotSecrets, 0o600))
			}

			wantGeneral, err := os.ReadFile(generalGoldenPath)
			require.NoError(t, err, "missing golden fixture — run with -update-golden to (re)generate")
			wantSecrets, err := os.ReadFile(secretsGoldenPath)
			require.NoError(t, err, "missing golden fixture — run with -update-golden to (re)generate")

			assert.Equal(t, string(wantGeneral), string(gotGeneral),
				"Render general.yaml output drifted from the golden fixture for %s", tc.name)
			assert.Equal(t, string(wantSecrets), string(gotSecrets),
				"Render secrets.yaml output drifted from the golden fixture for %s", tc.name)

			// The disk-writing path stayed in pkg/config/prompt, so the
			// "writeConfigFiles is a thin caller of Render" assertion moved
			// there with it — see TestWriteConfigFilesMatchesRender.
		})
	}
}

// TestRenderNilConfig proves Render returns an error instead of panicking
// on a nil cfg — the boundary an external caller (the Obmondo API) can
// reach that no CLI call site ever does.
func TestRenderNilConfig(t *testing.T) {
	general, secrets, err := Render(nil)
	assert.Error(t, err)
	assert.Nil(t, general)
	assert.Nil(t, secrets)
}

// TestRenderAlwaysEmitsGitKeyPathWithoutAgent pins the contract the Obmondo
// install flow depends on: when the operator is not on an SSH agent, the git
// block always carries a privateKeyFilePath line, even with no value.
//
// The portal ships that value empty on purpose — it cannot know where the key
// will land, because that depends on --configs-directory — and kubeaid-cli
// fills it in after writing the key. Omitting the line when the value is empty
// left nothing to fill, and the operator was asked for a key that had already
// been delivered.
//
// Hetzner's sshKeyPair and both ArgoCD deploy keys already behaved this way;
// git was the one that did not.
func TestRenderAlwaysEmitsGitKeyPathWithoutAgent(t *testing.T) {
	base := goldenCases()[0].cfg

	t.Run("empty path still emits the line", func(t *testing.T) {
		cfg := *base
		cfg.UseSSHAgent = false
		cfg.SSHKeyPath = ""

		general, _, err := Render(&cfg)
		require.NoError(t, err)
		assert.Contains(t, string(general), "\n  privateKeyFilePath:",
			"an empty path must still render the line, or the CLI has nothing to fill")
	})

	t.Run("a supplied path is rendered unchanged", func(t *testing.T) {
		cfg := *base
		cfg.UseSSHAgent = false
		cfg.SSHKeyPath = "/home/op/.ssh/id_ed25519"

		general, _, err := Render(&cfg)
		require.NoError(t, err)
		assert.Contains(t, string(general), "\n  privateKeyFilePath: /home/op/.ssh/id_ed25519")
	})

	t.Run("the agent path emits no line at all", func(t *testing.T) {
		cfg := *base
		cfg.UseSSHAgent = true
		cfg.SSHKeyPath = ""

		general, _, err := Render(&cfg)
		require.NoError(t, err)
		assert.NotContains(t, string(general), "\n  privateKeyFilePath:",
			"an agent operator has no key file, so the line would be a lie")
	})
}
