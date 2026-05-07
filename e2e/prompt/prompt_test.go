// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestBareMetal_PromptFlow tests the bare-metal provider prompt flow.
func TestBareMetal_PromptFlow(t *testing.T) {
	binary := buildTestBinary(t)
	sshKeyPath := setupDummySSHKey(t)
	outputDir := t.TempDir()
	c, cmd := newConsole(t, binary, outputDir)

	// Step 1 — Cluster basics group: provider + name (same group).
	// Bare-metal is index 3 in the provider select.
	c.expectString("Cloud provider:")
	c.selectOption(3)

	// Still in the same group — next field is Cluster name.
	c.expectString("Cluster name:")
	c.sendLine("e2e-baremetal")

	// Step 2 — OIDC form: decline OIDC (default No).
	c.expectString("Enable OIDC")
	c.acceptDefault()

	// Step 3 — Bare-metal has no credential form (sets defaults programmatically).

	// Step 4 — Git/SSH form: deploy key + config URL in one group,
	// then Git SSH key in a second group (no SSH agent).
	c.expectString("ArgoCD deploy key")
	c.sendLine(sshKeyPath)

	c.expectString("KubeAid Config fork URL:")
	c.acceptDefault()

	c.expectString("Your SSH private key")
	c.sendLine(sshKeyPath)

	// Summary confirm.
	c.expectString("Looks good?")
	c.acceptDefault()

	require.NoError(t, cmd.Wait(), "binary must exit cleanly")

	general := readGeneratedFile(t, outputDir, "general.yaml")

	// Cluster basics.
	assert.Contains(t, general, "name: e2e-baremetal")
	assert.Contains(t, general, "type: workload")
	assert.Contains(t, general, "enableAuditLogging: false")

	// Git / forks.
	assert.Contains(t, general, "sshUsername: git")
	assert.Contains(t, general, "useSSHAgent: false")
	assert.Contains(t, general, "directory:")

	// Bare-metal specifics.
	assert.Contains(t, general, "bare-metal:")
	assert.Contains(t, general, "port: 22")
	assert.Contains(t, general, "host: e2e-baremetal")
	assert.Contains(t, general, "port: 6443")
	assert.Contains(t, general, "privateKeyFilePath: "+sshKeyPath)

	// K8s version should be auto-detected.
	var generalMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(general), &generalMap))
	cluster, ok := generalMap["cluster"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, cluster["k8sVersion"], "k8sVersion should be auto-detected")
}

// TestLocal_PromptFlow tests the local provider prompt flow.
func TestLocal_PromptFlow(t *testing.T) {
	binary := buildTestBinary(t)
	sshKeyPath := setupDummySSHKey(t)
	outputDir := t.TempDir()
	c, cmd := newConsole(t, binary, outputDir)

	// Step 1 — Cluster basics group: provider + name (same group).
	// Local is index 4 in the provider select.
	c.expectString("Cloud provider:")
	c.selectOption(4)

	c.expectString("Cluster name:")
	c.sendLine("e2e-local")

	// Step 2 — OIDC form: decline OIDC (default No).
	c.expectString("Enable OIDC")
	c.acceptDefault()

	// Step 3 — Local has no credential form.

	// Step 4 — Git/SSH.
	c.expectString("ArgoCD deploy key")
	c.sendLine(sshKeyPath)

	c.expectString("KubeAid Config fork URL:")
	c.acceptDefault()

	c.expectString("Your SSH private key")
	c.sendLine(sshKeyPath)

	c.expectString("Looks good?")
	c.acceptDefault()

	require.NoError(t, cmd.Wait(), "binary must exit cleanly")

	general := readGeneratedFile(t, outputDir, "general.yaml")

	// Cluster basics.
	assert.Contains(t, general, "name: e2e-local")
	assert.Contains(t, general, "type: workload")

	// Local provider specific details.
	assert.Contains(t, general, "local: {}")

	// Fork / git details that must not be empty.
	assert.Contains(t, general, "forkURLs:")
	assert.Contains(t, general, "kubeaidConfig:")
	assert.Contains(t, general, "url: git@github.com:Obmondo/kubeaid-config.git")
	assert.Contains(t, general, "privateKeyFilePath: "+sshKeyPath)

	// K8s version should be auto-detected.
	var generalMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(general), &generalMap))
	cluster, ok := generalMap["cluster"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, cluster["k8sVersion"], "k8sVersion should be auto-detected")
}

// TestAWS_PromptFlow tests the AWS provider prompt flow.
func TestAWS_PromptFlow(t *testing.T) {
	binary := buildTestBinary(t)
	sshKeyPath := setupDummySSHKey(t)
	outputDir := t.TempDir()
	c, cmd := newConsole(t, binary, outputDir)

	// Step 1 — Cluster basics: AWS is index 0.
	c.expectString("Cloud provider:")
	c.selectOption(0)

	c.expectString("Cluster name:")
	c.sendLine("e2e-aws")

	// Step 2 — OIDC: decline (default No).
	c.expectString("Enable OIDC")
	c.acceptDefault()

	// Step 3 — AWS credentials form.
	// HOME is set to a scratch dir in newConsole so ~/.aws is empty.
	c.expectString("Access Key ID:")
	c.sendLine("AKIAIOSFODNN7EXAMPLE")

	c.expectString("Secret Access Key:")
	c.sendLine("wJalrXUtnFEMI/K7MDENG")

	c.expectString("Session Token")
	c.acceptDefault()

	// HA confirm is in the same credentials form, after the cred group.
	c.expectString("Enable high availability")
	c.acceptDefault()

	// After the credentials form returns, AMI lookup runs. If it fails
	// (no network in CI), a manual AMI input form is shown.
	nextPrompt := c.expectAnyString("Ubuntu 24.04 AMI ID for region", "ArgoCD deploy key")
	if nextPrompt == "Ubuntu 24.04 AMI ID for region" {
		c.sendLine("ami-0e2etestmanual123")
	}

	// Step 4 — Git/SSH.
	c.expectString("ArgoCD deploy key")
	c.sendLine(sshKeyPath)

	c.expectString("KubeAid Config fork URL:")
	c.acceptDefault()

	c.expectString("Your SSH private key")
	c.sendLine(sshKeyPath)

	c.expectString("Looks good?")
	c.acceptDefault()

	require.NoError(t, cmd.Wait(), "binary must exit cleanly")

	general := readGeneratedFile(t, outputDir, "general.yaml")
	secrets := readGeneratedFile(t, outputDir, "secrets.yaml")

	// Cluster basics.
	assert.Contains(t, general, "name: e2e-aws")
	assert.Contains(t, general, "type: workload")
	assert.Contains(t, general, "enableAuditLogging: false")

	// Git / forks.
	assert.Contains(t, general, "sshUsername: git")
	assert.Contains(t, general, "directory:")

	// AWS specifics.
	assert.Contains(t, general, "region: eu-west-1")
	assert.Contains(t, general, "instanceType: t3.medium")
	assert.Contains(t, general, "replicas: 3")
	assert.Contains(t, general, "bastionEnabled: true")
	assert.Contains(t, general, "loadBalancerScheme: internet-facing")

	keyName := strings.TrimSuffix(filepath.Base(sshKeyPath), filepath.Ext(sshKeyPath))
	assert.Contains(t, general, "sshKeyName: "+keyName)

	// AMI ID should always be set: auto-detected from Canonical or entered manually.
	assert.Contains(t, general, "ami:")
	assert.Contains(t, general, "id: ami-")

	// K8s version should be auto-detected.
	var generalMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(general), &generalMap))
	cluster, ok := generalMap["cluster"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, cluster["k8sVersion"], "k8sVersion should be auto-detected")

	// Secrets.
	assert.Contains(t, secrets, "accessKeyID: AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, secrets, "secretAccessKey: wJalrXUtnFEMI/K7MDENG")
	assert.Contains(t, secrets, "sessionToken:")
}

// TestAzure_PromptFlow tests the Azure provider prompt flow.
func TestAzure_PromptFlow(t *testing.T) {
	binary := buildTestBinary(t)
	sshKeyPath := setupDummySSHKey(t)
	outputDir := t.TempDir()
	c, cmd := newConsole(t, binary, outputDir)

	// Step 1 — Cluster basics: Azure is index 1.
	c.expectString("Cloud provider:")
	c.selectOption(1)

	c.expectString("Cluster name:")
	c.sendLine("e2eazure")

	// Step 2 — OIDC: decline (default No).
	c.expectString("Enable OIDC")
	c.acceptDefault()

	// Step 3 — Azure credentials (all in one group + HA confirm).
	c.expectString("Tenant ID:")
	c.sendLine("tenant-123")

	c.expectString("Subscription ID:")
	c.sendLine("sub-456")

	c.expectString("Client ID:")
	c.sendLine("client-id-123")

	c.expectString("Client Secret:")
	c.sendLine("client-secret-456")

	c.expectString("Enable high availability")
	c.acceptDefault()

	// Step 4 — Git/SSH.
	c.expectString("ArgoCD deploy key")
	c.sendLine(sshKeyPath)

	c.expectString("KubeAid Config fork URL:")
	c.acceptDefault()

	c.expectString("Your SSH private key")
	c.sendLine(sshKeyPath)

	c.expectString("Looks good?")
	c.acceptDefault()

	require.NoError(t, cmd.Wait(), "binary must exit cleanly")

	general := readGeneratedFile(t, outputDir, "general.yaml")
	secrets := readGeneratedFile(t, outputDir, "secrets.yaml")

	// Cluster basics.
	assert.Contains(t, general, "name: e2eazure")
	assert.Contains(t, general, "type: workload")
	assert.Contains(t, general, "enableAuditLogging: false")

	// Git / forks.
	assert.Contains(t, general, "sshUsername: git")
	assert.Contains(t, general, "directory:")

	// Azure specifics.
	assert.Contains(t, general, "tenantID: tenant-123")
	assert.Contains(t, general, "subscriptionID: sub-456")
	assert.Contains(t, general, "location: westeurope")
	assert.Contains(t, general, "vmSize: Standard_B2s")
	assert.Contains(t, general, "diskSizeGB: 128")
	assert.Contains(t, general, "replicas: 3")
	assert.Contains(t, general, "storageAccount: e2eazuresa")
	assert.Contains(t, general, "loadBalancerType: Public")

	// K8s version should be auto-detected.
	var generalMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(general), &generalMap))
	cluster, ok := generalMap["cluster"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, cluster["k8sVersion"], "k8sVersion should be auto-detected")

	// Secrets.
	assert.Contains(t, secrets, "clientID: client-id-123")
	assert.Contains(t, secrets, "clientSecret: client-secret-456")
}

// TestHetznerHCloud_PromptFlow tests the Hetzner hcloud provider prompt flow.
func TestHetznerHCloud_PromptFlow(t *testing.T) {
	binary := buildTestBinary(t)
	sshKeyPath := setupDummySSHKey(t)
	outputDir := t.TempDir()
	c, cmd := newConsole(t, binary, outputDir)

	// Step 1 — Cluster basics: Hetzner is index 2.
	c.expectString("Cloud provider:")
	c.selectOption(2)

	c.expectString("Cluster name:")
	c.sendLine("e2e-hetzner")

	// Cluster-kind group appears for Hetzner; workload is the default.
	c.expectString("What are you setting up?")
	c.acceptDefault()

	// Step 2 — OIDC: decline (default No; VPN would auto-configure OIDC).
	c.expectString("Enable OIDC")
	c.acceptDefault()

	// Step 3 — Hetzner credentials form: mode + token + SSH key + HA.
	c.expectString("Mode:")
	c.acceptDefault()

	c.expectString("Cloud API token:")
	c.sendLine("hetzner-token-abc")

	c.expectString("SSH private key file path:")
	c.sendLine(sshKeyPath)

	c.expectString("Enable high availability")
	c.acceptDefault()

	// Step 4 — Git/SSH.
	c.expectString("ArgoCD deploy key")
	c.sendLine(sshKeyPath)

	c.expectString("KubeAid Config fork URL:")
	c.acceptDefault()

	c.expectString("Your SSH private key")
	c.sendLine(sshKeyPath)

	c.expectString("Looks good?")
	c.acceptDefault()

	require.NoError(t, cmd.Wait(), "binary must exit cleanly")

	general := readGeneratedFile(t, outputDir, "general.yaml")
	secrets := readGeneratedFile(t, outputDir, "secrets.yaml")

	// Cluster basics.
	assert.Contains(t, general, "name: e2e-hetzner")
	assert.Contains(t, general, "type: workload")
	assert.Contains(t, general, "enableAuditLogging: false")

	// Git / forks.
	assert.Contains(t, general, "sshUsername: git")
	assert.Contains(t, general, "directory:")

	// Hetzner specifics.
	assert.Contains(t, general, "mode: hcloud")
	assert.Contains(t, general, "zone: eu-central")
	assert.Contains(t, general, "machineType: cax21")
	assert.Contains(t, general, "replicas: 3")
	assert.Contains(t, general, "region: hel1")
	assert.Contains(t, general, "imageName: ubuntu-24.04")
	assert.Contains(t, general, "cidr: \"10.0.0.0/16\"")
	assert.Contains(t, general, "hcloudServersSubnetCIDR: \"10.0.0.0/24\"")

	keyName := strings.TrimSuffix(filepath.Base(sshKeyPath), filepath.Ext(sshKeyPath))
	assert.Contains(t, general, "name: "+keyName)

	// K8s version should be auto-detected.
	var generalMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(general), &generalMap))
	cluster, ok := generalMap["cluster"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, cluster["k8sVersion"], "k8sVersion should be auto-detected")

	// Secrets.
	assert.Contains(t, secrets, "apiToken: hetzner-token-abc")
}
