// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"testing"

	"github.com/creasty/defaults"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/render"
)

// TestRenderedConfigPassesValidation closes the loop between the two halves
// of the config path: pkg/render writes general.yaml and secrets.yaml, this
// package reads them back and validates them, and until now nothing checked
// that the first produces something the second accepts.
//
// The golden fixtures in pkg/render pin Render's output byte-for-byte, but a
// fixture can be regenerated with -update-golden, so blessing broken output
// is one flag away. This test cannot be silenced that way: it re-parses what
// Render actually produced and runs the same struct-tag validation the CLI
// runs on a real config file.
//
// It matters more than it looks. A template edit that shifts indentation by
// two spaces still parses as YAML — required fields would trip notblank, but
// optional pointer blocks (KubePrometheus, Obmondo, ACMEDNS01, Lockdown)
// unmarshal to nil in silence, which ships a cluster with monitoring quietly
// off.
func TestRenderedConfigPassesValidation(t *testing.T) {
	lockdown := false

	cfg := &render.PromptedConfig{
		SSHUsername:                "git",
		UseSSHAgent:                true,
		KubeaidForkURL:             "https://github.com/Obmondo/kubeaid.git",
		KubeaidVersion:             "31.0.4",
		KubeaidConfigForkURL:       "git@github.com:acme/kubeaid-config.git",
		KubeaidConfigDeployKeyPath: "/tmp/ssh-priv",
		ClusterName:                "hcloud-acme",
		ClusterType:                "workload",
		K8sVersion:                 "v1.35.6",
		Lockdown:                   &lockdown,

		CloudProvider:        "hetzner",
		HetznerMode:          "hcloud",
		HetznerSSHKeyName:    "hcloud-acme",
		HetznerHCloudZone:    "eu-central",
		HetznerCPMachineType: "cax21",
		HetznerCPReplicas:    "3",
		HetznerRegion:        "hel1",
		HetznerLBRegion:      "hel1",
		HetznerAPIToken:      "fake-hcloud-token",
	}

	generalYAML, secretsYAML, err := render.Render(cfg)
	require.NoError(t, err, "rendering must succeed before anything can be validated")

	// Mirrors ParseConfigFiles: unmarshal, then apply defaults, then
	// validate. Defaults matter — several fields carry both a default and
	// notblank, so validating a raw unmarshal would fail on values the real
	// parse path fills in.
	var (
		generalConfig config.GeneralConfig
		secretsConfig config.SecretsConfig
	)
	//nolint:musttag // same as ParseConfigFiles: GeneralConfig's yaml tags
	// are on its nested structs, which musttag does not follow.
	require.NoError(t, yaml.Unmarshal(generalYAML, &generalConfig),
		"rendered general.yaml must unmarshal into GeneralConfig")
	require.NoError(t, yaml.Unmarshal(secretsYAML, &secretsConfig),
		"rendered secrets.yaml must unmarshal into SecretsConfig")

	require.NoError(t, defaults.Set(&generalConfig))

	require.NoError(t, validateConfigStructTags(&generalConfig, &secretsConfig),
		"rendered config must satisfy the validation the CLI applies when it reads one back")
}
