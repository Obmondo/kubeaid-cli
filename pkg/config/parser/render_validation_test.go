// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/creasty/defaults"
	gogitClient "github.com/go-git/go-git/v5/plumbing/transport/client"
	gogitHTTP "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/render"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
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

	generalConfig, secretsConfig := parseRenderedConfig(t, generalYAML, secretsYAML)

	require.NoError(t, validateConfigStructTags(generalConfig, secretsConfig),
		"rendered config must satisfy the validation the CLI applies when it reads one back")
}

// Mirrors ParseConfigFiles: unmarshal, then apply defaults. Defaults matter —
// several fields carry both a default and notblank, so validating a raw
// unmarshal would fail on values the real parse path fills in.
func parseRenderedConfig(t *testing.T, generalYAML, secretsYAML []byte) (*config.GeneralConfig, *config.SecretsConfig) {
	t.Helper()

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

	return &generalConfig, &secretsConfig
}

type hcloudServerTypes struct {
	cloud.CloudProvider
}

func (hcloudServerTypes) GetVMSpecs(_ context.Context, machineType string) (*cloud.VMSpec, error) {
	if machineType != "cax21" {
		return nil, fmt.Errorf("machine type %q not found", machineType)
	}
	return &cloud.VMSpec{CPU: 4, Memory: 8}, nil
}

type kubeAidTagAdvertisement string

func (tag kubeAidTagAdvertisement) RoundTrip(req *http.Request) (*http.Response, error) {
	const commit = "1111111111111111111111111111111111111111"
	body := pktLine("# service=git-upload-pack\n") + "0000" +
		pktLine(commit+" refs/tags/"+string(tag)+"\x00\n") + "0000"

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/x-git-upload-pack-advertisement"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func pktLine(line string) string {
	return fmt.Sprintf("%04x%s", len(line)+4, line)
}

func TestRenderedSingleNodeConfigsPassValidation(t *testing.T) {
	stubK8sVersionDeps(t)

	originalHTTPS := gogitClient.Protocols["https"]
	t.Cleanup(func() { gogitClient.InstallProtocol("https", originalHTTPS) })
	gogitClient.InstallProtocol("https", gogitHTTP.NewClient(&http.Client{Transport: kubeAidTagAdvertisement("38.1.1")}))

	originalProviderName, originalProvider := globals.CloudProviderName, globals.CloudProvider
	t.Cleanup(func() { globals.CloudProviderName, globals.CloudProvider = originalProviderName, originalProvider })
	globals.CloudProvider = hcloudServerTypes{}

	generalPaths, err := filepath.Glob(filepath.Join("..", "..", "render", "testdata", "golden", "*-single-node.general.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, generalPaths, "no single-node golden found")

	for _, generalPath := range generalPaths {
		golden := strings.TrimSuffix(generalPath, ".general.yaml")
		t.Run(filepath.Base(golden), func(t *testing.T) {
			generalYAML, err := os.ReadFile(generalPath)
			require.NoError(t, err)
			secretsYAML, err := os.ReadFile(golden + ".secrets.yaml")
			require.NoError(t, err)

			generalConfig, secretsConfig := parseRenderedConfig(t, generalYAML, secretsYAML)
			withParsedConfig(t, generalConfig, secretsConfig)
			detectCloudProviderName()

			require.NoError(t, validateConfigs(t.Context()))
			assert.True(t, kubernetes.IsNodeGroupCountZero(t.Context()),
				"a cluster with no worker servers must get its control-plane taint removed")
		})
	}
}
