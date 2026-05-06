// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// oidcDiscoveryTimeout caps the pre-bootstrap discovery probe. It's
// short on purpose — we want a fast fail when the issuer URL is
// mistyped or unreachable, not a slow bootstrap that times out
// minutes later.
const oidcDiscoveryTimeout = 10 * time.Second

// ValidateOIDCDiscovery probes the configured OIDC issuer's discovery
// endpoint (.well-known/openid-configuration) so a misconfigured URL
// fails fast — before any infrastructure is provisioned. No-op when
// the cluster has no apiServer.oidc block.
//
// Responsibilities:
//   - HTTP GET <issuer>/.well-known/openid-configuration
//   - Verify the response is JSON with an "issuer" field that matches
//     the configured IssuerURL (per the OIDC discovery spec)
//   - Surface DNS / TLS / timeout / HTTP-status errors as actionable
//     messages instead of letting them flow through later in bootstrap
//
// When apiServer.oidc.caBundlePath is set, the probe trusts that PEM
// for TLS — so the same CA bundle that kube-apiserver will use also
// gates this pre-flight check.
func ValidateOIDCDiscovery(ctx context.Context) error {
	cfg := config.ParsedGeneralConfig.Cluster.APIServer.OIDC
	if cfg == nil {
		return nil
	}

	// Managed Keycloak is provisioned by THIS bootstrap run — its
	// realm endpoint isn't reachable yet (the cluster doesn't
	// exist; the keycloakx chart hasn't synced; cert-manager
	// hasn't issued the TLS cert that the operator's DNS will
	// point at). Probing here would always fail with a TLS-name
	// mismatch (default traefik cert) or NXDOMAIN. Skip — kubeaid-cli
	// re-probes via the in-cluster port-forward later in the
	// bootstrap pipeline once Keycloak is actually up.
	if kc := config.ParsedGeneralConfig.Cluster.Keycloak; kc != nil && kc.Mode == constants.KeycloakModeManaged {
		return nil
	}

	client, err := buildOIDCDiscoveryClient(cfg.CABundlePath)
	if err != nil {
		return err
	}

	discoveryURL := strings.TrimRight(cfg.IssuerURL, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("preparing OIDC discovery request for %s: %w", cfg.IssuerURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return categoriseOIDCDiscoveryError(err, cfg.IssuerURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"OIDC issuer %s returned HTTP %d on %s — check apiServer.oidc.issuerUrl",
			cfg.IssuerURL, resp.StatusCode, discoveryURL,
		)
	}

	var doc struct {
		Issuer string `json:"issuer"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf(
			"OIDC discovery for %s: response body is not valid JSON: %w",
			cfg.IssuerURL, err,
		)
	}

	// OIDC discovery spec (§4): the issuer in the discovery document
	// MUST exactly match the URL the request was issued against,
	// modulo a trailing slash. Catches misconfigured Keycloak realms
	// where the public URL and the realm's "Frontend URL" disagree.
	if strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(cfg.IssuerURL, "/") {
		return fmt.Errorf(
			"OIDC discovery: issuer mismatch — config has %q but the discovery document reports %q",
			cfg.IssuerURL, doc.Issuer,
		)
	}

	return nil
}

// buildOIDCDiscoveryClient returns an http.Client configured with the
// user-supplied CA bundle (if any). When CABundlePath is empty the
// client falls back to the system trust store.
func buildOIDCDiscoveryClient(caBundlePath string) (*http.Client, error) {
	if caBundlePath == "" {
		return &http.Client{Timeout: oidcDiscoveryTimeout}, nil
	}

	pem, err := os.ReadFile(caBundlePath)
	if err != nil {
		return nil, fmt.Errorf("reading OIDC CA bundle %q: %w", caBundlePath, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("OIDC CA bundle %q has no valid PEM certs", caBundlePath)
	}

	return &http.Client{
		Timeout: oidcDiscoveryTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}, nil
}

// categoriseOIDCDiscoveryError unwraps a Go HTTP error to figure out
// which of the common failure modes the user hit, and returns a
// single-line error suitable for the bootstrap logs.
func categoriseOIDCDiscoveryError(err error, issuer string) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Errorf(
			"OIDC issuer hostname %q is not resolvable (issuer URL: %s) — check apiServer.oidc.issuerUrl",
			dnsErr.Name, issuer,
		)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf(
			"OIDC issuer %s did not respond within %s (network or VPN issue?)",
			issuer, oidcDiscoveryTimeout,
		)
	}

	// crypto/tls error types are often wrapped behind url.Error; a
	// substring check here is more reliable than errors.As.
	if strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "tls:") {
		return fmt.Errorf(
			"OIDC issuer %s TLS error: %w (set apiServer.oidc.caBundlePath if Keycloak uses a private CA)",
			issuer, err,
		)
	}

	return fmt.Errorf("OIDC discovery for %s failed: %w", issuer, err)
}
