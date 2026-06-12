// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// oidcDiscoveryTimeout caps the pre-flight discovery probe. Short on purpose:
// we want a fast, clear failure on a mistyped or unreachable issuer, not a
// long hang before the browser flow.
const oidcDiscoveryTimeout = 10 * time.Second

// probeIssuerDiscovery fetches <issuerURL>/.well-known/openid-configuration
// and verifies it is a 200 whose `issuer` field matches issuerURL. It turns a
// wrong oidc.issuerUrl (typo, missing /auth base path, realm-name casing) into
// a fast, actionable error — naming the realm's canonical issuer when the
// document loads — instead of the cryptic failure kubelogin surfaces mid-flow
// once the browser dance has already started.
//
// TLS is verified against the system trust store: the cluster CA in klist
// covers kube-apiserver's serving cert, not the (separate) Keycloak host.
func probeIssuerDiscovery(ctx context.Context, issuerURL string) error {
	discoveryURL := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("preparing OIDC discovery request for %s: %w", issuerURL, err)
	}

	client := &http.Client{Timeout: oidcDiscoveryTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return categoriseDiscoveryError(err, issuerURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"OIDC discovery: issuer returned HTTP %d on %s — the oidc.issuerUrl is "+
				"wrong (check the base path, e.g. /auth, and the realm name's casing)",
			resp.StatusCode, discoveryURL,
		)
	}

	var doc struct {
		Issuer string `json:"issuer"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf(
			"OIDC discovery for %s: response is not valid JSON: %w "+
				"(is oidc.issuerUrl pointing at a Keycloak realm?)",
			issuerURL, err,
		)
	}

	// OIDC discovery spec (§4): the issuer in the document MUST exactly match
	// the URL the request was issued against, modulo a trailing slash. This
	// catches a realm whose configured public URL and "Frontend URL" disagree,
	// and casing/path slips that still resolve to a live realm.
	if strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(issuerURL, "/") {
		return fmt.Errorf(
			"OIDC discovery: issuer mismatch — oidc.issuerUrl is %q but the realm's "+
				"canonical issuer is %q; use the canonical value",
			issuerURL, doc.Issuer,
		)
	}

	return nil
}

// categoriseDiscoveryError unwraps a Go HTTP error into one of the common
// failure modes and returns a single-line, action-oriented message. Mirrors
// the bootstrap-side probe (pkg/config/parser/oidc_discovery.go) so the two
// surface consistent guidance.
func categoriseDiscoveryError(err error, issuer string) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Errorf(
			"OIDC issuer hostname %q is not resolvable (issuerUrl: %s) — check the URL "+
				"or your DNS / NetBird mesh",
			dnsErr.Name, issuer,
		)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf(
			"OIDC issuer %s did not respond within %s (network or NetBird path slow — retry?)",
			issuer, oidcDiscoveryTimeout,
		)
	}

	// crypto/tls error types are usually wrapped behind url.Error; a substring
	// check is more reliable than errors.As here.
	if strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "tls:") {
		return fmt.Errorf(
			"OIDC issuer %s TLS error: %w (Keycloak's cert is not trusted by your system store)",
			issuer, err,
		)
	}

	return fmt.Errorf("OIDC discovery for %s failed: %w", issuer, err)
}
