// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/health"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/httpx"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/iris"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/wazuh"
)

// probeTimeout bounds each health probe.
const probeTimeout = 15 * time.Second

// Health component names.
const (
	HealthKeycloak     = "keycloak"
	HealthIRIS         = "iris"
	HealthVelociraptor = "velociraptor"
)

// Probe reads the platform health: every configured indexer (cluster
// status, disk), every tenant manager (agent counts, analysisd event
// loss) and the reachability of Keycloak, IRIS and the Velociraptor
// API. It only reads; a failed probe is recorded in the snapshot, never
// returned as an error.
func Probe(ctx context.Context, opts Options) health.Snapshot {
	cfg := opts.Config
	store := secrets.Store{Kube: opts.Kube}
	snap := health.Snapshot{Time: time.Now().UTC()}

	for _, w := range cfg.Components.Wazuh {
		if w.Indexer != nil {
			snap.Indexers = append(snap.Indexers, probeIndexer(ctx, store, w.Tenant, *w.Indexer))
		}
		snap.Managers = append(snap.Managers, probeManager(ctx, store, w))
	}
	if wc := cfg.Components.WazuhCentral; wc != nil {
		snap.Indexers = append(snap.Indexers, probeIndexer(ctx, store, CentralScope, config.Indexer{
			URL: wc.URL, CredSecretRef: wc.CredSecretRef, CAFile: wc.CAFile, InsecureSkipVerify: wc.InsecureSkipVerify,
		}))
	}
	snap.Components = append(snap.Components, probeKeycloak(ctx, cfg.Keycloak))
	if ic := cfg.Components.IRIS; ic != nil {
		snap.Components = append(snap.Components, probeIRIS(ctx, store, *ic))
	}
	if vc := cfg.Components.Velociraptor; vc != nil && vc.Address != "" {
		snap.Components = append(snap.Components, probeTCP(ctx, HealthVelociraptor, vc.Address))
	}
	return snap
}

func probeIndexer(ctx context.Context, store secrets.Store, name string, ix config.Indexer) health.Indexer {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := health.Indexer{Name: name}
	client, err := indexerClient(ctx, store, ix)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	h, err := client.Health(ctx)
	out.Status, out.DiskUsedPercent, out.DiskUsedBytes, out.DiskTotalBytes = h.Status, h.DiskUsedPercent, h.DiskUsedBytes, h.DiskTotalBytes
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Up = true
	return out
}

func probeManager(ctx context.Context, store secrets.Store, w config.Wazuh) health.Manager {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := health.Manager{Tenant: w.Tenant}
	user, pass, err := readCreds(ctx, store, w.CredSecretRef)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	hc, err := httpx.Client(w.CAFile, w.InsecureSkipVerify)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	client := &wazuh.Client{BaseURL: w.URL, Username: user, Password: pass, HTTP: hc}
	a, err := client.AgentSummary(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Up = true
	out.AgentsActive, out.AgentsDisconnected, out.AgentsNeverConnected = a.Active, a.Disconnected, a.NeverConnected
	out.AgentsPending, out.AgentsTotal = a.Pending, a.Total
	if s, err := client.AnalysisdStats(ctx); err == nil {
		out.StatsAvailable = true
		out.EventsDropped, out.QueueUsageMax = s.EventsDropped, s.QueueUsageMax
	} else {
		out.Error = "analysisd stats: " + err.Error()
	}
	return out
}

// probeKeycloak fetches the realm's OIDC discovery document, which
// needs no credentials.
func probeKeycloak(ctx context.Context, kc config.Keycloak) health.Component {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := health.Component{Name: HealthKeycloak}
	hc, err := httpx.Client(kc.CAFile, kc.InsecureSkipVerify)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	u := strings.TrimRight(kc.URL, "/") + "/realms/" + kc.Realm + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	resp, err := hc.Do(req)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out.Error = fmt.Sprintf("OIDC discovery: HTTP %d", resp.StatusCode)
		return out
	}
	out.Up = true
	return out
}

func probeIRIS(ctx context.Context, store secrets.Store, ic config.IRIS) health.Component {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := health.Component{Name: HealthIRIS}
	key, err := store.Read(ctx, ic.APIKeySecretRef)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	hc, err := httpx.Client(ic.CAFile, ic.InsecureSkipVerify)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if err := (&iris.Client{BaseURL: ic.URL, APIKey: key, HTTP: hc}).Ping(ctx); err != nil {
		out.Error = err.Error()
		return out
	}
	out.Up = true
	return out
}

// probeTCP checks that address accepts connections.
func probeTCP(ctx context.Context, name, address string) health.Component {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := health.Component{Name: name}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	_ = conn.Close()
	out.Up = true
	return out
}
