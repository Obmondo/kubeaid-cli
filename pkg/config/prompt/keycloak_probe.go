// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

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

// promptOIDCProbeTimeout caps how long the prompt waits on the
// Keycloak discovery endpoint before failing the form. Short on
// purpose — operators expect prompt-time validation to be snappy,
// and the most common failure (NetBird VPN not up) shows as DNS
// or connect refusal well within this bound.
const promptOIDCProbeTimeout = 10 * time.Second

// probeOIDCIssuerErrKind classifies the failure mode so the prompt
// can render an actionable hint (NetBird offline vs DNS typo vs TLS
// vs realm 404). Mirrors the categorisation parser/oidc_discovery.go
// does at bootstrap, but the failure surfaces matter more here because
// the operator can still edit values inline.
type probeOIDCIssuerErrKind string

const (
	probeOIDCErrUnresolvable   probeOIDCIssuerErrKind = "unresolvable"
	probeOIDCErrTimeout        probeOIDCIssuerErrKind = "timeout"
	probeOIDCErrConnRefused    probeOIDCIssuerErrKind = "conn-refused"
	probeOIDCErrTLS            probeOIDCIssuerErrKind = "tls"
	probeOIDCErrHTTPStatus     probeOIDCIssuerErrKind = "http-status"
	probeOIDCErrBadJSON        probeOIDCIssuerErrKind = "bad-json"
	probeOIDCErrIssuerMismatch probeOIDCIssuerErrKind = "issuer-mismatch"
	probeOIDCErrOther          probeOIDCIssuerErrKind = "other"
)

// probeOIDCIssuerError carries the kind classification + the issuer
// URL we tried, so renderProbeOIDCError can format a single
// operator-facing message without re-deriving the URL.
type probeOIDCIssuerError struct {
	Issuer string
	Kind   probeOIDCIssuerErrKind
	Status int    // populated when Kind == probeOIDCErrHTTPStatus
	Got    string // populated when Kind == probeOIDCErrIssuerMismatch
	Cause  error
}

func (e *probeOIDCIssuerError) Error() string {
	return fmt.Sprintf("OIDC discovery (%s) on %s: %v", e.Kind, e.Issuer, e.Cause)
}

func (e *probeOIDCIssuerError) Unwrap() error { return e.Cause }

// probeOIDCIssuerFn is the indirection that lets tests stub the probe
// without standing up an httptest server for every form-flow case.
// Defaults to the real network call.
var probeOIDCIssuerFn = realProbeOIDCIssuer

// probeOIDCIssuer is the function the prompt code calls; tests assign
// probeOIDCIssuerFn to override.
func probeOIDCIssuer(ctx context.Context, dns, realm string) error {
	return probeOIDCIssuerFn(ctx, dns, realm)
}

// realProbeOIDCIssuer performs a single GET against the realm's OIDC
// discovery endpoint and returns a *probeOIDCIssuerError on failure.
// Uses the system trust store — operators with a private CA point
// kube-apiserver at it via apiServer.oidc.caBundlePath in general.yaml,
// but the prompt only probes public realms (the typical case is a
// parent VPN's Keycloak with Let's Encrypt).
func realProbeOIDCIssuer(ctx context.Context, dns, realm string) error {
	issuerURL := "https://" + dns + "/realms/" + realm
	discoveryURL := issuerURL + "/.well-known/openid-configuration"

	probeCtx, cancel := context.WithTimeout(ctx, promptOIDCProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return &probeOIDCIssuerError{Issuer: issuerURL, Kind: probeOIDCErrOther, Cause: err}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &probeOIDCIssuerError{
			Issuer: issuerURL,
			Kind:   classifyTransportErr(err),
			Cause:  err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &probeOIDCIssuerError{
			Issuer: issuerURL,
			Kind:   probeOIDCErrHTTPStatus,
			Status: resp.StatusCode,
			Cause:  fmt.Errorf("HTTP %d on %s", resp.StatusCode, discoveryURL),
		}
	}

	var doc struct {
		Issuer string `json:"issuer"`
	}
	if jsonErr := json.NewDecoder(resp.Body).Decode(&doc); jsonErr != nil {
		return &probeOIDCIssuerError{
			Issuer: issuerURL,
			Kind:   probeOIDCErrBadJSON,
			Cause:  jsonErr,
		}
	}

	if strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(issuerURL, "/") {
		return &probeOIDCIssuerError{
			Issuer: issuerURL,
			Kind:   probeOIDCErrIssuerMismatch,
			Got:    doc.Issuer,
			Cause:  fmt.Errorf("discovery doc reports issuer %q", doc.Issuer),
		}
	}

	return nil
}

// classifyTransportErr maps a transport-level error to a kind tag.
// DNS / timeout / connection refused / TLS each get their own bucket
// so renderProbeOIDCError can speak to the most-likely root cause.
func classifyTransportErr(err error) probeOIDCIssuerErrKind {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return probeOIDCErrUnresolvable
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return probeOIDCErrTimeout
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return probeOIDCErrConnRefused
	case strings.Contains(msg, "x509"), strings.Contains(msg, "tls:"):
		return probeOIDCErrTLS
	}

	return probeOIDCErrOther
}

// renderProbeOIDCError formats a multi-line, operator-facing message
// shown above the retry/skip confirm. The first line names what
// failed; the bulleted list narrows down the likely cause so the
// operator knows whether to fix DNS, reconnect NetBird, or pick a
// different realm. Falls through to err.Error() for kinds we don't
// have a specific hint for.
func renderProbeOIDCError(err error) string {
	var pe *probeOIDCIssuerError
	if !errors.As(err, &pe) {
		return err.Error()
	}

	var header, body string
	switch pe.Kind {
	case probeOIDCErrUnresolvable:
		header = fmt.Sprintf("DNS lookup failed for %s", pe.Issuer)
		body = "" +
			"  • Are you connected to the NetBird VPN? (`netbird status`)\n" +
			"  • Is the Keycloak DNS correct?"
	case probeOIDCErrTimeout:
		header = fmt.Sprintf("Timed out reaching %s after %s", pe.Issuer, promptOIDCProbeTimeout)
		body = "" +
			"  • NetBird mesh path may be slow or partitioned\n" +
			"  • Keycloak may be reachable but overloaded"
	case probeOIDCErrConnRefused:
		header = fmt.Sprintf("Connection refused to %s", pe.Issuer)
		body = "" +
			"  • Keycloak isn't listening on that address\n" +
			"  • Wrong port, wrong host, or Keycloak is down"
	case probeOIDCErrTLS:
		header = fmt.Sprintf("TLS error reaching %s", pe.Issuer)
		body = "" +
			"  • Keycloak certificate isn't trusted by your system\n" +
			"  • If Keycloak uses a private CA, edit general.yaml after\n" +
			"    the prompt and add apiServer.oidc.caBundlePath."
	case probeOIDCErrHTTPStatus:
		header = fmt.Sprintf("Keycloak returned HTTP %d on %s/.well-known/openid-configuration", pe.Status, pe.Issuer)
		body = "" +
			"  • Realm name is likely wrong (404 is the common case)\n" +
			"  • Keycloak may be misconfigured"
	case probeOIDCErrIssuerMismatch:
		header = fmt.Sprintf("Keycloak's discovery doc reports a different issuer: %q", pe.Got)
		body = "" +
			"  • Keycloak's Frontend URL is set to something other than\n" +
			"    " + pe.Issuer + "\n" +
			"  • Fix Frontend URL in Keycloak, or use whichever URL\n" +
			"    Keycloak reports as canonical."
	case probeOIDCErrBadJSON:
		header = fmt.Sprintf("Discovery endpoint at %s returned non-JSON", pe.Issuer)
		body = "" +
			"  • Probably an HTML error page from a reverse proxy\n" +
			"  • Make sure /realms/<realm> reaches Keycloak directly."
	case probeOIDCErrOther:
		return pe.Error()
	}

	return header + "\n\n" + body
}
