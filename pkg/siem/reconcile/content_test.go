// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

func TestContentTargetsCanaryFirst(t *testing.T) {
	t.Parallel()
	cfg := loadExample(t)
	cfg.Components.Content = &config.Content{Dir: "x", StateNamespace: "security-operations", Canary: "002"}
	targets := ContentTargets(context.Background(), cfg, secrets.Store{Kube: fake.NewClientset()})
	require.Len(t, targets, 2)
	assert.Equal(t, "002", targets[0].Tenant)
	assert.Error(t, targets[0].Err, "no credentials in the fake cluster")

	cfg.Components.Content.Canary = ""
	assert.Equal(t, "001", ContentTargets(context.Background(), cfg, secrets.Store{Kube: fake.NewClientset()})[0].Tenant)

	only, err := ParseOnly("content")
	require.NoError(t, err)
	assert.Equal(t, []string{ComponentContent}, only)
}

func TestRunContentReportsPerManager(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "VERSION"), []byte("1\n"), 0o600))
	cfg := loadExample(t)
	cfg.Components.Content = &config.Content{Dir: dir, StateNamespace: "security-operations", StatePrefix: "kubesoc-content-"}
	results := Run(context.Background(), Options{Config: cfg, Kube: fake.NewClientset(), DryRun: true, Only: []string{ComponentContent}})
	byComponent := map[string]report.Action{}
	for _, r := range results {
		byComponent[r.Component] = r.Action
	}
	assert.Equal(t, map[string]report.Action{"content/001": report.ActionError, "content/002": report.ActionError}, byComponent,
		"no manager credentials: an error each, and a dry run does not skip the others")
}
