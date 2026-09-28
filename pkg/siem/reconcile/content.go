// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/content"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/httpx"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuh"
)

// runContent rolls the content package out: every tenant manager
// (canary first), then the Velociraptor artifacts.
func runContent(ctx context.Context, cfg *config.Config, store secrets.Store, dryRun bool) []report.Result {
	cc := cfg.Components.Content
	bundle, err := content.Load(cc.Dir, cc.ArtifactDirs)
	if err != nil {
		return errorResult(ComponentContent, "package", err)
	}
	state := content.SecretStore{Kube: store.Kube, Namespace: cc.StateNamespace, Prefix: cc.StatePrefix}
	results := content.RolloutWazuh(ctx, ContentTargets(ctx, cfg, store), bundle, state, dryRun)
	if cc.Velociraptor && len(bundle.Artifacts) > 0 {
		client, kind, err := dialVelociraptor(ctx, cfg, store)
		if err != nil {
			return append(results, errorResult(content.ComponentVelociraptor, kind, err)...)
		}
		defer func() { _ = client.Close() }()
		results = append(results, content.RolloutArtifacts(ctx, client, bundle, state, dryRun)...)
	}
	return results
}

// ContentTargets lists the tenant managers, the canary first.
func ContentTargets(ctx context.Context, cfg *config.Config, store secrets.Store) []content.Target {
	canary := cfg.Components.Content.Canary
	var first, rest []content.Target
	for _, wc := range cfg.Components.Wazuh {
		t := content.Target{Tenant: wc.Tenant}
		if user, pass, err := readCreds(ctx, store, wc.CredSecretRef); err != nil {
			t.Err = err
		} else if hc, err := httpx.Client(wc.CAFile, wc.InsecureSkipVerify); err != nil {
			t.Err = err
		} else {
			t.API = &wazuh.Client{BaseURL: wc.URL, Username: user, Password: pass, HTTP: hc}
		}
		if (canary == "" && first == nil) || wc.Tenant == canary {
			first = append(first, t)
		} else {
			rest = append(rest, t)
		}
	}
	return append(first, rest...)
}
