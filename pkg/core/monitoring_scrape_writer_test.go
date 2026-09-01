// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddScrapeNamespaces(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		missing []string

		expectChanged bool
		expectErr     bool
		expectContain []string // substrings that must appear in the result
	}{
		{
			name: "empty list, matches cluster-vars.jsonnet.tmpl's freshly-generated shape",
			content: `{
  platform: "kubeadm",
  prometheus_scrape_namespaces: [],
  prometheus_scrape_default_namespaces: [
    "argocd",
  ],
}
`,
			missing:       []string{"openobserve"},
			expectChanged: true,
			expectContain: []string{`"openobserve"`},
		},
		{
			// The shape every real *-vars.jsonnet file we've seen actually has, once an
			// operator has hand-edited it (e.g. k8s/vpn/vpn-vars.jsonnet).
			name: "multi-line hand-edited list, matches real vpn-vars.jsonnet shape",
			content: `{
  prometheus_scrape_namespaces: [
    "cloudnative-pg",
    "external-dns",
    "netbird",
    "keycloakx",
  ],
  prometheus_scrape_default_namespaces: [
    "argocd",
    "sealed-secrets",
    "cert-manager",
  ],
}
`,
			missing:       []string{"velero"},
			expectChanged: true,
			expectContain: []string{`"cloudnative-pg"`, `"external-dns"`, `"netbird"`, `"keycloakx"`, `"velero"`},
		},
		{
			name: "namespace already listed is left alone and reported unchanged",
			content: `{
  prometheus_scrape_namespaces: [
    "netbird",
  ],
}
`,
			missing:       []string{"netbird"},
			expectChanged: false,
		},
		{
			// This codebase uses both quote styles across clusters (double-quoted on master's
			// vpn-vars.jsonnet, single-quoted on the hetzner-qa branch's) - an existing
			// single-quoted entry must still be recognised, or this would insert a duplicate.
			name: "namespace already listed with single quotes is recognised, not duplicated",
			content: `{
  prometheus_scrape_namespaces: [
    'argocd',
    'netbird',
  ],
}
`,
			missing:       []string{"netbird"},
			expectChanged: false,
		},
		{
			name: "only the genuinely-missing namespace out of several is added",
			content: `{
  prometheus_scrape_namespaces: [
    "netbird",
  ],
}
`,
			missing:       []string{"netbird", "velero"},
			expectChanged: true,
			expectContain: []string{`"netbird"`, `"velero"`},
		},
		{
			name: "doesn't touch prometheus_scrape_default_namespaces",
			content: `{
  prometheus_scrape_namespaces: [
    "netbird",
  ],
  prometheus_scrape_default_namespaces: [
    "argocd",
  ],
}
`,
			missing:       []string{"velero"},
			expectChanged: true,
			expectContain: []string{`"velero"`},
		},
		{
			name: "field not found is an error, not a silent no-op",
			content: `{
  platform: "kubeadm",
}
`,
			missing:   []string{"velero"},
			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			updated, changed, err := insertScrapeNamespaces(testCase.content, testCase.missing)

			if testCase.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.expectChanged, changed)

			if !testCase.expectChanged {
				assert.Equal(t, testCase.content, updated)
				return
			}
			for _, substr := range testCase.expectContain {
				assert.Contains(t, updated, substr)
			}

			// The result must still be a valid-looking bracketed list (every entry between
			// the same "prometheus_scrape_namespaces: [" ... "]" pair) - regression guard
			// against the regex accidentally spilling past the closing bracket into
			// prometheus_scrape_default_namespaces or another field.
			match := scrapeNamespacesFieldPattern.FindStringSubmatch(updated)
			require.NotNil(t, match)
			for _, ns := range testCase.missing {
				assert.Contains(t, match[2], ns)
			}
		})
	}
}

// Re-running the fix against its own output must be a no-op the second time - a repeat
// bootstrap/upgrade shouldn't grow the list every time it runs.
func TestAddScrapeNamespaces_Idempotent(t *testing.T) {
	content := `{
  prometheus_scrape_namespaces: [
    "netbird",
  ],
}
`
	once, changed, err := insertScrapeNamespaces(content, []string{"velero"})
	require.NoError(t, err)
	require.True(t, changed)

	twice, changedAgain, err := insertScrapeNamespaces(once, []string{"velero"})
	require.NoError(t, err)
	assert.False(t, changedAgain)
	assert.Equal(t, once, twice)
}
