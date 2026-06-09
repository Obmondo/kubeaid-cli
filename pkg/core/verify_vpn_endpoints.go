// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/keycloak"
)

// verifyVPNClusterEndpoints sanity-checks that the user-facing endpoints
// of a managed-Keycloak VPN cluster (Keycloak realm, NetBird dashboard,
// NetBird Mgmt API) are actually responding before bootstrap declares
// itself complete. Without this, a misconfigured OIDC scope or missing
// audience mapper still produces a green "Bootstrap complete" panel
// because the underlying Pods are Ready — the operator only discovers
// SSO is broken on first login, long after kubeaid-cli has exited and
// the control-plane LB's public interface has been disabled.
//
// Each check is retried with a short backoff so cert-manager has a
// moment to issue the Let's Encrypt cert without us flaking. Final
// failures are collected and returned together, so the operator sees
// every broken endpoint at once instead of fixing them one at a time.
//
// No-op when the cluster is not a managed-Keycloak VPN cluster.
func verifyVPNClusterEndpoints(ctx context.Context) error {
	if !vpnClusterEnabled() || !managedKeycloakEnabled() {
		return nil
	}
	cluster := config.ParsedGeneralConfig.Cluster
	if cluster.Keycloak == nil || cluster.NetBird == nil {
		return nil
	}

	checks := []endpointCheck{
		{
			label: "Keycloak OpenID config",
			url: fmt.Sprintf("https://%s/auth/realms/%s/.well-known/openid-configuration",
				cluster.Keycloak.DNS, cluster.Keycloak.Realm),
			validate: validateKeycloakOpenIDConfig,
		},
		{
			label:    "NetBird dashboard",
			url:      fmt.Sprintf("https://%s/", cluster.NetBird.DNS),
			validate: expectStatus(http.StatusOK),
		},
		{
			// /api/users without a token returns 401 — that's the
			// success signal (server is reachable and enforcing
			// auth, which is what we want). A 502/503 means Mgmt
			// isn't actually up.
			label:    "NetBird Mgmt API",
			url:      fmt.Sprintf("https://%s/api/users", cluster.NetBird.DNS),
			validate: expectStatus(http.StatusUnauthorized),
		},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var failures []string
	for _, c := range checks {
		if err := c.runWithRetry(ctx, client); err != nil {
			failures = append(failures, fmt.Sprintf("  - %s (%s): %s", c.label, c.url, err))
			slog.WarnContext(ctx, "VPN cluster endpoint check failed",
				slog.String("label", c.label),
				slog.String("url", c.url),
				slog.Any("err", err),
			)
			continue
		}
		slog.InfoContext(ctx, "VPN cluster endpoint check OK", slog.String("label", c.label))
	}

	if len(failures) > 0 {
		return fmt.Errorf("VPN cluster endpoint verification failed — fix and re-run:\n%s",
			strings.Join(failures, "\n"))
	}
	return nil
}

type endpointCheck struct {
	label    string
	url      string
	validate func(*http.Response) error
}

// runWithRetry tries the check up to 6 times with 10s between attempts
// (~1 minute total). cert-manager normally finishes the HTTP-01 ACME
// challenge in well under a minute on a fresh bootstrap, so this absorbs
// the gap without dragging out a real failure.
func (c endpointCheck) runWithRetry(ctx context.Context, client *http.Client) error {
	const maxAttempts = 6
	const interval = 10 * time.Second

	var last error
	for i := 0; i < maxAttempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.do(ctx, client); err == nil {
			return nil
		} else {
			last = err
		}
		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
		}
	}
	return last
}

func (c endpointCheck) do(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.validate(resp)
}

func expectStatus(want int) func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp.StatusCode != want {
			return fmt.Errorf("HTTP %d (want %d)", resp.StatusCode, want)
		}
		return nil
	}
}

// validateKeycloakOpenIDConfig checks 200 + scopes_supported includes
// the netbird scope. If the scope is missing, the Keycloak reconciler
// didn't finish — exactly the class of bug we shipped a fix for in the
// same series (chart asked for a scope kubeaid-cli never created), and
// would have been caught here before the operator saw it on login.
func validateKeycloakOpenIDConfig(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d (want 200)", resp.StatusCode)
	}
	var body struct {
		ScopesSupported []string `json:"scopes_supported"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decoding OpenID config: %w", err)
	}
	for _, s := range body.ScopesSupported {
		if s == keycloak.NetBirdAPIScopeName {
			return nil
		}
	}
	return errors.New("realm missing the netbird api scope — Keycloak reconciler did not finish")
}
