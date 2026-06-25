// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sUnstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// ---- helpers -----------------------------------------------------------

// buildFakeClient returns a controller-runtime fake client populated with the
// given objects (the core/v1 scheme, for Nodes, is registered).
func buildFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

// makeNode builds a Node with the given ExternalIP (omitted when empty). Set
// isCP=true to add the control-plane role label.
func makeNode(name, externalIP string, isCP bool) *coreV1.Node {
	n := &coreV1.Node{ObjectMeta: metaV1.ObjectMeta{Name: name}}
	if externalIP != "" {
		n.Status.Addresses = []coreV1.NodeAddress{
			{Type: coreV1.NodeExternalIP, Address: externalIP},
		}
	}
	if isCP {
		n.Labels = map[string]string{"node-role.kubernetes.io/control-plane": ""}
	}
	return n
}

// ---- warnIfSSHWorldOpen ------------------------------------------------

func TestWarnIfSSHWorldOpen(t *testing.T) {
	// The function only emits a warning — verify it doesn't panic on any
	// combination of nil/non-nil config.
	tests := []struct {
		name    string
		hetzner *config.HetznerConfig
	}{
		{name: "nil hetzner config", hetzner: nil},
		{name: "no bareMetal block", hetzner: &config.HetznerConfig{}},
		{
			name: "bareMetal with empty allowSshFrom",
			hetzner: &config.HetznerConfig{
				BareMetal: &config.HetznerBareMetalConfig{},
			},
		},
		{
			name: "bareMetal with allowSshFrom set",
			hetzner: &config.HetznerConfig{
				BareMetal: &config.HetznerBareMetalConfig{
					Firewall: config.FirewallConfig{
						AllowSSHFrom: []string{"192.0.2.0/24"},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFreshGeneralConfig(t, func() {
				config.ParsedGeneralConfig.Cloud.Hetzner = tc.hetzner
				assert.NotPanics(t, func() {
					warnIfSSHWorldOpen(context.Background())
				})
			})
		})
	}
}

// ---- checkNodeIPsPresent -----------------------------------------------

func TestCheckNodeIPsPresent(t *testing.T) {
	tests := []struct {
		name    string
		nodes   []client.Object
		wantErr string
	}{
		{
			name: "nodes with ExternalIP: pass",
			nodes: []client.Object{
				makeNode("cp-0", "192.0.2.1", true),
				makeNode("w-0", "192.0.2.2", false),
			},
		},
		{
			name:    "no nodes: error",
			nodes:   nil,
			wantErr: "no node ExternalIP found",
		},
		{
			name:    "nodes without ExternalIP: error",
			nodes:   []client.Object{makeNode("cp-0", "", true)},
			wantErr: "no node ExternalIP found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := buildFakeClient(t, tc.nodes...)
			err := checkNodeIPsPresent(context.Background(), c)
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

// ---- flipHostNetworkPolicyEnabled --------------------------------------

func TestFlipHostNetworkPolicyEnabled(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantContains string
		wantErr      string
	}{
		{
			name: "flips disabled to enabled",
			input: `hostNetworkPolicy:
  enabled: false
  allowSshFrom: []
`,
			wantContains: "  enabled: true",
		},
		{
			name: "already enabled: idempotent no-op",
			input: `hostNetworkPolicy:
  enabled: true
`,
			wantContains: "  enabled: true",
		},
		{
			name: "no enabled line at all: returns error",
			input: `hostNetworkPolicy:
  allowSshFrom: []
`,
			wantErr: `hostNetworkPolicy block present`,
		},
		{
			name: "only first occurrence flipped (extra indented enabled lines untouched)",
			input: `hostNetworkPolicy:
  enabled: false
# comment: enabled: false should not be touched here
`,
			wantContains: "  enabled: true",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "values-cilium-*.yaml")
			require.NoError(t, err)
			_, err = f.WriteString(tc.input)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			err = flipHostNetworkPolicyEnabled(f.Name())
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)

			got, readErr := os.ReadFile(f.Name())
			require.NoError(t, readErr)
			assert.Contains(t, string(got), tc.wantContains)
			assert.NotContains(t, string(got), "  enabled: false")
		})
	}
}

// ---- flipHostNetworkPolicyEnabled — git repo integration ---------------

func TestFlipHostNetworkPolicyEnabledInTempRepo(t *testing.T) {
	// Simulate the full rendered output of values-cilium.yaml.tmpl:
	// the template renders two "enabled:" lines — one 4-space inside
	// cilium.hostFirewall (value: true) and one 2-space in hostNetworkPolicy
	// (value: false). Only the 2-space hostNetworkPolicy line must be flipped.
	dir := t.TempDir()
	valuesPath := filepath.Join(dir, "values-cilium.yaml")
	contents := `---
cilium:
  kubeProxyReplacement: "true"
  k8sServiceHost: cp.acme.com
  k8sServicePort: 6443
  hostFirewall:
    enabled: true
  extraArgs:
    - "--devices=eth+,enp+,eno+"
hostNetworkPolicy:
  enabled: false
  allowSshFrom: []
  apiserverSourceCIDRs:
    - 192.0.2.1/32
`
	require.NoError(t, os.WriteFile(valuesPath, []byte(contents), 0o600))

	require.NoError(t, flipHostNetworkPolicyEnabled(valuesPath))

	got, err := os.ReadFile(valuesPath)
	require.NoError(t, err)
	gotStr := string(got)

	// The 2-space hostNetworkPolicy line is now true.
	assert.Contains(t, gotStr, "  enabled: true")
	// The 2-space false line is gone.
	assert.NotContains(t, gotStr, "  enabled: false")
	// The 4-space cilium.hostFirewall line is unchanged.
	assert.Contains(t, gotStr, "    enabled: true")
	// Other fields must be preserved.
	assert.Contains(t, gotStr, "allowSshFrom: []")
	assert.Contains(t, gotStr, "192.0.2.1/32")
	assert.Contains(t, gotStr, "k8sServiceHost: cp.acme.com")
}

// ---- readAllowSshFrom --------------------------------------------------

func TestReadAllowSshFrom(t *testing.T) {
	tests := []struct {
		name    string
		content string // empty = file does not exist
		wantStr string // substring expected in the return value
		noFile  bool
	}{
		{
			name:    "file does not exist",
			noFile:  true,
			wantStr: "unreadable",
		},
		{
			name:    "malformed YAML",
			content: "hostNetworkPolicy: :\n  :",
			wantStr: "unreadable",
		},
		{
			name: "allowSshFrom not set",
			content: `hostNetworkPolicy:
  enabled: false
`,
			wantStr: "not set",
		},
		{
			name: "allowSshFrom empty list",
			content: `hostNetworkPolicy:
  allowSshFrom: []
`,
			wantStr: "not set",
		},
		{
			name: "allowSshFrom populated",
			content: `hostNetworkPolicy:
  allowSshFrom:
    - 192.0.2.0/24
    - 198.51.100.5
`,
			wantStr: "restricted to",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			if tc.noFile {
				path = "/tmp/nonexistent-values-cilium-test.yaml"
			} else {
				f, err := os.CreateTemp(t.TempDir(), "values-cilium-*.yaml")
				require.NoError(t, err)
				_, err = f.WriteString(tc.content)
				require.NoError(t, err)
				require.NoError(t, f.Close())
				path = f.Name()
			}
			got := readAllowSshFrom(path)
			assert.Contains(t, got, tc.wantStr)
		})
	}
}

// ---- extractCCNPFromManifest -------------------------------------------

func TestExtractCCNPFromManifest(t *testing.T) {
	const validCCNP = `---
apiVersion: cilium.io/v2
kind: CiliumClusterwideNetworkPolicy
metadata:
  name: kubeaid-host-firewall
spec:
  ingress:
    - fromEntities:
        - host
        - remote-node
`

	tests := []struct {
		name     string
		manifest string
		wantName string
		wantErr  string
	}{
		{
			name:     "single CCNP document",
			manifest: validCCNP,
			wantName: "kubeaid-host-firewall",
		},
		{
			name: "CCNP among other documents",
			manifest: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: some-config
  namespace: kube-system
data:
  key: value
---
` + strings.TrimPrefix(validCCNP, "---\n"),
			wantName: "kubeaid-host-firewall",
		},
		{
			name: "no CCNP in manifest",
			manifest: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: some-config
  namespace: kube-system
data:
  key: value
`,
			wantErr: "no CiliumClusterwideNetworkPolicy document found",
		},
		{
			name:     "empty manifest",
			manifest: "",
			wantErr:  "no CiliumClusterwideNetworkPolicy document found",
		},
		{
			name: "malformed YAML document: error returned",
			manifest: `---
: invalid: yaml: document
  bad indentation
	tabs: mixed
`,
			wantErr: "decoding YAML document",
		},
		{
			name: "CCNP with realistic acme.com CIDRs",
			manifest: `---
apiVersion: cilium.io/v2
kind: CiliumClusterwideNetworkPolicy
metadata:
  name: kubeaid-host-firewall
spec:
  ingress:
    - fromEntities:
        - host
        - remote-node
        - health
        - cluster
        - kube-apiserver
    - fromCIDR:
        - 192.0.2.1/32
        - 198.51.100.2/32
        - 203.0.113.3/32
      toPorts:
        - ports:
            - port: "6443"
              protocol: TCP
    - fromEntities:
        - world
      toPorts:
        - ports:
            - port: "80"
              protocol: TCP
            - port: "443"
              protocol: TCP
`,
			wantName: "kubeaid-host-firewall",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := extractCCNPFromManifest(tc.manifest)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, obj)
			assert.Equal(t, tc.wantName, obj.GetName())
			assert.Equal(t, ciliumCCNPKind, obj.GetKind())
			assert.Equal(t, ciliumCCNPAPIVersion, obj.GetAPIVersion())
		})
	}
}

// ---- TestHelmRenderManifestFakeRender ----------------------------------

// TestHelmRenderManifestFakeRender verifies that extractCCNPFromManifest
// correctly extracts the CCNP from a canned render that would be returned
// by HelmRenderManifest. This tests the integration of the two functions
// without requiring a real Helm chart on disk.
func TestHelmRenderManifestFakeRender(t *testing.T) {
	// Canned output mimicking what a real cilium chart render would produce
	// when hostNetworkPolicy.enabled=true — just the CCNP document plus
	// some surrounding chart noise.
	cannedRender := `---
# Source: cilium/templates/cilium-agent/daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cilium
  namespace: kube-system
spec:
  selector:
    matchLabels:
      k8s-app: cilium
---
# Source: cilium/templates/network-policy/host-firewall-policy.yaml
apiVersion: cilium.io/v2
kind: CiliumClusterwideNetworkPolicy
metadata:
  name: kubeaid-host-firewall
spec:
  ingress:
    - fromEntities:
        - host
        - remote-node
        - health
        - cluster
        - kube-apiserver
    - fromCIDR:
        - 192.0.2.10/32
      toPorts:
        - ports:
            - port: "6443"
              protocol: TCP
    - fromEntities:
        - world
      toPorts:
        - ports:
            - port: "80"
              protocol: TCP
            - port: "443"
              protocol: TCP
---
# Source: cilium/templates/cilium-operator/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cilium-operator
  namespace: kube-system
`

	obj, err := extractCCNPFromManifest(cannedRender)
	require.NoError(t, err)
	require.NotNil(t, obj)
	assert.Equal(t, "kubeaid-host-firewall", obj.GetName())
	assert.Equal(t, ciliumCCNPKind, obj.GetKind())
	assert.Equal(t, ciliumCCNPAPIVersion, obj.GetAPIVersion())

	// Verify the ingress structure was decoded correctly.
	ingress, found, err := k8sUnstructured.NestedSlice(obj.Object, "spec", "ingress")
	require.NoError(t, err)
	assert.True(t, found, "spec.ingress should be present")
	assert.Len(t, ingress, 3, "expected 3 ingress rules: identity + apiserver + world")
}

// ---- errLockdownDeclined sentinel --------------------------------------

// TestErrLockdownDeclined verifies that the sentinel error is distinct from
// other errors and that errors.Is works for caller disambiguation.
func TestErrLockdownDeclined(t *testing.T) {
	require.True(t, errors.Is(errLockdownDeclined, errLockdownDeclined))
	wrapped := fmt.Errorf("outer: %w", errLockdownDeclined)
	require.True(t, errors.Is(wrapped, errLockdownDeclined),
		"errors.Is must traverse the wrap chain")

	other := fmt.Errorf("some other error")
	require.False(t, errors.Is(other, errLockdownDeclined))
}

// ---- firstCPNodeIP -----------------------------------------------------

func TestFirstCPNodeIP(t *testing.T) {
	tests := []struct {
		name   string
		nodes  []client.Object
		wantIP string
	}{
		{
			name: "returns a control-plane node's ExternalIP (sorted)",
			nodes: []client.Object{
				makeNode("cp-b", "198.51.100.2", true),
				makeNode("cp-a", "192.0.2.1", true),
				makeNode("worker", "203.0.113.9", false),
			},
			// sorted CP ExternalIPs: 192.0.2.1 < 198.51.100.2; worker excluded
			wantIP: "192.0.2.1",
		},
		{
			name:   "no nodes: placeholder",
			nodes:  nil,
			wantIP: "<cp-node-ip>",
		},
		{
			name:   "only workers have an ExternalIP: placeholder",
			nodes:  []client.Object{makeNode("worker", "203.0.113.9", false)},
			wantIP: "<cp-node-ip>",
		},
		{
			name:   "CP node without ExternalIP: placeholder",
			nodes:  []client.Object{makeNode("cp-a", "", true)},
			wantIP: "<cp-node-ip>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := buildFakeClient(t, tc.nodes...)
			got := firstCPNodeIP(context.Background(), c)
			assert.Equal(t, tc.wantIP, got)
		})
	}
}
