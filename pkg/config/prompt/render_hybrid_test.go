// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	validatorV10 "github.com/go-playground/validator/v10"
	nonStandardValidators "github.com/go-playground/validator/v10/non-standard/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// TestRenderHetznerHybridVPN locks in the previously-broken hybrid
// mode rendering. Before this branch, picking mode=hybrid in the
// prompt left BareMetal.VSwitch nil, which made
// prerequisite_infrastructure.go's CreateVSwitch call panic at
// bootstrap. This drives the template with a realistic hybrid VPN
// config (HCloud CP + bare-metal worker node group + vSwitch) and
// asserts validator.Struct passes — same path bootstrap takes.
//
// Uses cluster.type=vpn so the LB endpoint is rendered (validator
// tag `required,fqdn`). Workload-mode hybrid has a separate
// pre-existing template gap where endpoint is blank.
func TestRenderHetznerHybridVPN(t *testing.T) {
	cfg := &PromptedConfig{
		SSHUsername:          "git",
		UseSSHAgent:          true,
		KubeaidForkURL:       "https://github.com/Obmondo/kubeaid.git",
		KubeaidVersion:       "29.0.9",
		KubeaidConfigForkURL: "git@github.com:acme/kubeaid-config.git",
		ClusterName:          "hybrid-acme",
		// VPN cluster type exercises the LoadBalancer.Endpoint render
		// path (required,fqdn). Workload mode leaves it blank — a
		// pre-existing template gap that pre-dates the hybrid UX work
		// and affects pure-hcloud workload renders too. Tracked
		// separately; not in scope for this regression test.
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
		HetznerSSHKeyName:    "kbm-hybrid",
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
	}

	dir := t.TempDir()
	require.NoError(t, writeConfigFiles(dir, cfg))

	body, err := os.ReadFile(filepath.Join(dir, "general.yaml"))
	require.NoError(t, err)
	general := string(body)
	t.Logf("--- rendered general.yaml (hybrid) ---\n%s", general)

	// Both hcloud (CP) and bareMetal (workers + vSwitch) must render.
	assert.Contains(t, general, "mode: hybrid")
	assert.Contains(t, general, "hcloud:")
	assert.Contains(t, general, "zone: eu-central")
	assert.Contains(t, general, "machineType: cax21")
	assert.Contains(t, general, "bareMetal:")
	assert.Contains(t, general, "vSwitch:")
	assert.Contains(t, general, "name: hybrid-acme-vswitch")
	assert.Contains(t, general, "vlanID: 4001")
	assert.Contains(t, general, `subnetCIDRBlock: "10.0.1.0/24"`)

	// Worker node group renders alongside an empty hcloud node group.
	assert.Contains(t, general, "hcloud: []")
	assert.Contains(t, general, "name: workers")
	assert.Contains(t, general, `serverID: "1234570"`)
	assert.Contains(t, general, "privateIP: 10.0.1.10")
	assert.Contains(t, general, "Robot main IP: 5.5.5.10")

	// Hybrid mode should NOT render bare-metal CP/endpoint blocks —
	// the control plane lives in HCloud.
	assert.NotContains(t, general, "isFailoverIP:",
		"hybrid mode CP is HCloud — no bareMetal endpoint expected")

	parsed := &config.GeneralConfig{}
	//nolint:musttag // GeneralConfig has hydrated runtime fields without yaml tags by design — same waiver as pkg/config/parser/parse.go.
	require.NoError(t, yaml.Unmarshal(body, parsed))

	v := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	require.NoError(t, v.RegisterValidation("notblank", nonStandardValidators.NotBlank))

	require.NotNil(t, parsed.Cloud.Hetzner)
	assert.NoError(t, v.Struct(parsed.Cloud.Hetzner),
		"rendered hybrid Hetzner block must satisfy validate tags — this is the bootstrap path that previously panicked")

	// vSwitch must be non-nil so CreateVSwitch doesn't deref nil at
	// bootstrap time.
	require.NotNil(t, parsed.Cloud.Hetzner.BareMetal, "bareMetal block must render for hybrid")
	require.NotNil(t, parsed.Cloud.Hetzner.BareMetal.VSwitch, "vSwitch must render for hybrid")
	assert.Equal(t, "hybrid-acme-vswitch", parsed.Cloud.Hetzner.BareMetal.VSwitch.Name)
	assert.Equal(t, 4001, parsed.Cloud.Hetzner.BareMetal.VSwitch.VLANID)
	assert.Equal(t, "10.0.1.0/24", parsed.Cloud.Hetzner.BareMetal.VSwitch.SubnetCIDRBlock)
}
