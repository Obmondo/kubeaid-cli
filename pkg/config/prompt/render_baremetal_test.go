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

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

// TestRenderHetznerBareMetalWorkload renders the prompt templates
// against a realistic acme-style config and asserts the resulting
// general.yaml carries the bare-metal block — proves the validation
// failure the operator hit in the prompt UX is fixed
// (cloud.hetzner.controlPlane.bareMetal + bareMetalHosts now render).
func TestRenderHetznerBareMetalWorkload(t *testing.T) {
	cfg := &PromptedConfig{
		SSHUsername:                "git",
		UseSSHAgent:                true,
		KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
		KubeaidVersion:             "29.0.9",
		KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
		ClusterName:                "bm-acme",
		ClusterType:                "workload",
		K8sVersion:                 "v1.35.4",
		EnableOIDC:                 true,
		OIDCIssuerURL:              "https://keycloak.vpn.acme.com/auth/realms/acme",
		OIDCClientID:               "kubernetes-bm-acme",
		KeycloakMode:               "external",
		KeycloakDNS:                "keycloak.vpn.acme.com",
		KeycloakRealm:              "acme",
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
		HetznerBMNodeGroupName:        "bm-acme-workers",
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
		HetznerRobotUser:     "fake-user",
		HetznerRobotPassword: "fake-pass",
	}

	dir := t.TempDir()
	require.NoError(t, writeConfigFiles(dir, cfg))

	body, err := os.ReadFile(filepath.Join(dir, "general.yaml"))
	require.NoError(t, err)
	general := string(body)
	t.Logf("--- rendered general.yaml ---\n%s", general)

	// Sanity: hcloud fields must NOT leak into a bare-metal render
	// (the bug that prevented `kubeaid-cli cluster bootstrap` from
	// passing validation).
	assert.NotContains(t, general, "machineType:", "hcloud machineType leaked into bare-metal render")
	assert.NotContains(t, general, "loadBalancer:", "hcloud loadBalancer block leaked into bare-metal render")
	assert.NotContains(t, general, "hcloudServersSubnetCIDR", "hcloud subnet leaked into bare-metal render")

	// Bare-metal control plane.
	assert.Contains(t, general, "bareMetal:")
	assert.Contains(t, general, "isFailoverIP: true")
	assert.Contains(t, general, "host: 1.2.3.4")
	assert.Contains(t, general, `serverID: "1234567"`)
	assert.Contains(t, general, `serverID: "1234568"`)
	assert.Contains(t, general, `serverID: "1234569"`)
	assert.Contains(t, general, "privateIP: 10.0.1.1")
	assert.Contains(t, general, "privateIP: 10.0.1.2")
	assert.Contains(t, general, "privateIP: 10.0.1.3")
	// Robot lookup IPs appear as informational comments.
	assert.Contains(t, general, "Robot main IP: 5.5.5.1")

	// vSwitch block rendered so the operator has the network plumbing
	// captured alongside the cluster — follow-up branch will wire
	// kubeaid-cli's CreateVSwitch into the pure-BM path and pick this
	// up automatically.
	assert.Contains(t, general, "vSwitch:")
	assert.Contains(t, general, "name: bm-acme-vswitch")
	assert.Contains(t, general, "vlanID: 4002")
	assert.Contains(t, general, `subnetCIDRBlock: "10.0.1.0/24"`)

	// Bare-metal node group.
	assert.Contains(t, general, "name: bm-acme-workers")
	assert.Contains(t, general, `serverID: "1234570"`)
	assert.Contains(t, general, "privateIP: 10.0.1.10")

	// Cluster-level fields rendered the same way as hcloud.
	assert.Contains(t, general, "name: bm-acme")
	assert.Contains(t, general, "type: workload")
	assert.Contains(t, general, "mode: bare-metal")
	assert.Contains(t, general, "issuerUrl: https://keycloak.vpn.acme.com/auth/realms/acme")

	secrets, err := os.ReadFile(filepath.Join(dir, "secrets.yaml"))
	require.NoError(t, err)
	// Values are %q-quoted in secrets.yaml so Hetzner Robot tokens
	// containing YAML metacharacters (#, :, leading -, etc.) survive
	// round-tripping; see render_secrets_test.go for the bug-fix
	// regression cases.
	assert.Contains(t, string(secrets), `user: "fake-user"`)
	assert.Contains(t, string(secrets), `password: "fake-pass"`)

	// The crux of the original bug: the rendered general.yaml must
	// pass struct-level validation. validateConfigStructTags is the
	// exact path bootstrap took when it produced
	// `'GeneralConfig.Cloud.Hetzner.ControlPlane' Error:Field validation for 'ControlPlane' failed on the 'required' tag`.
	parsed := &config.GeneralConfig{}
	//nolint:musttag // GeneralConfig has hydrated runtime fields without yaml tags by design — same waiver as pkg/config/parser/parse.go.
	require.NoError(t, yaml.Unmarshal(body, parsed),
		"rendered general.yaml should unmarshal cleanly")

	v := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	require.NoError(t, v.RegisterValidation("notblank", nonStandardValidators.NotBlank))

	// Validating the Cloud.Hetzner sub-struct in isolation keeps the
	// test focused on the bug we're fixing (the rest of the struct
	// has required ArgoCD / Forks / etc. that aren't load-bearing
	// for this regression).
	require.NotNil(t, parsed.Cloud.Hetzner, "rendered YAML must include cloud.hetzner")
	err = v.Struct(parsed.Cloud.Hetzner)
	assert.NoError(t, err, "rendered Hetzner bare-metal block must satisfy validate tags")
}
