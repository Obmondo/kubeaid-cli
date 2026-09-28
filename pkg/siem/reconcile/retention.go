// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/httpx"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/indexer"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

// CentralScope names the central indexer in retention and health output.
const CentralScope = "central"

// runRetention keeps the retention policy on every tenant indexer
// (the tenant's retentionDays) and on the central one (centralDays).
// A failing indexer is reported and the next one runs.
func runRetention(ctx context.Context, cfg *config.Config, store secrets.Store, dryRun bool) []report.Result {
	var results []report.Result
	days := map[string]int{}
	for _, t := range cfg.Tenants {
		days[t.Code] = t.RetentionDays
	}
	for _, w := range cfg.Components.Wazuh {
		if w.Indexer == nil {
			continue
		}
		component := indexer.ComponentName(w.Tenant)
		client, err := indexerClient(ctx, store, *w.Indexer)
		if err != nil {
			results = append(results, errorResult(component, "indexer", err)...)
			continue
		}
		results = append(results, indexer.Reconcile(ctx, client, component, RetentionPolicy(cfg, days[w.Tenant], false), dryRun)...)
	}
	if wc := cfg.Components.WazuhCentral; wc != nil && cfg.Retention.CentralDays > 0 {
		component := indexer.ComponentName(CentralScope)
		client, err := indexerClient(ctx, store, config.Indexer{
			URL: wc.URL, CredSecretRef: wc.CredSecretRef, CAFile: wc.CAFile, InsecureSkipVerify: wc.InsecureSkipVerify,
		})
		if err != nil {
			return append(results, errorResult(component, "indexer", err)...)
		}
		results = append(results, indexer.Reconcile(ctx, client, component, RetentionPolicy(cfg, cfg.Retention.CentralDays, true), dryRun)...)
	}
	return results
}

// RetentionPolicy derives the ISM policy of one indexer.
func RetentionPolicy(cfg *config.Config, days int, central bool) indexer.Policy {
	r := cfg.Retention
	p := indexer.Policy{ID: r.PolicyID, RetentionDays: days, WarmAfterDays: r.WarmAfterDays, IndexPatterns: r.IndexPatterns}
	if central {
		p.IndexPatterns = r.CentralIndexPatterns
	}
	return p
}

func indexerClient(ctx context.Context, store secrets.Store, ix config.Indexer) (*indexer.Client, error) {
	user, pass, err := readCreds(ctx, store, ix.CredSecretRef)
	if err != nil {
		return nil, err
	}
	hc, err := httpx.Client(ix.CAFile, ix.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}
	return &indexer.Client{BaseURL: ix.URL, Username: user, Password: pass, HTTP: hc}, nil
}
