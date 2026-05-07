// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"text/template"

	"golang.org/x/net/publicsuffix"
)

// deriveRealmFromDNS returns the first dot-separated segment of the
// effective TLD-plus-one for host. Returns "" when host has no public
// suffix or is otherwise unworkable — the parser's validator surfaces
// the error at parse time.
//
// Mirrors parser.deriveRealm; duplicated here to avoid an import cycle
// (parser imports config; prompt is upstream of both at config-write
// time).
func deriveRealmFromDNS(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	etldPlusOne, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}

	return strings.SplitN(etldPlusOne, ".", 2)[0]
}

// stripFirstLabel returns host with its leading DNS label removed —
// "keycloak.vpn.acme.com" → "vpn.acme.com". Used by the prompt to
// turn the operator-supplied Keycloak DNS into a base domain it can
// prefix with "netbird." / "api." for the next prompts' defaults.
//
// Returns "" when host has no dot (single label like "localhost"),
// in which case auto-derivation is skipped and the operator types
// each FQDN explicitly.
func stripFirstLabel(host string) string {
	host = strings.TrimSpace(host)
	idx := strings.Index(host, ".")
	if idx < 0 {
		return ""
	}
	return host[idx+1:]
}

// deriveACMEEmailFromDNS returns "ops@<eTLD+1>" for host — e.g.
// "vpn.obmondo.com" → "ops@obmondo.com". Used as a default for the
// LE contact email so the operator can press Enter when their domain
// already has an ops@ alias. publicsuffix handles multi-part TLDs
// correctly; "" on lookup failure so the operator just types it.
func deriveACMEEmailFromDNS(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	etldPlusOne, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}

	return "ops@" + etldPlusOne
}

// errRequired is returned by the nonEmpty validator when the input is empty.
var errRequired = errors.New("value is required")

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errRequired
	}
	return nil
}

// httpsURL validates that s is a non-empty https:// URL with a host.
// Used for inputs where the protocol matters at bootstrap time
// (e.g. OIDC issuer URLs — kube-apiserver only fetches JWKS over
// TLS).
func httpsURL(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}

	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "https" {
		return errors.New("URL must start with https://")
	}

	if u.Host == "" {
		return errors.New("URL must include a host (https://<host>/...)")
	}

	return nil
}

// writeTemplatedFile renders a Go template string with the given data and writes it to disk.
func writeTemplatedFile(filePath string, tmplStr string, data any, perm os.FileMode) error {
	dir := path.Dir(filePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", filePath, err)
	}
	defer f.Close()

	tmpl, err := template.New(path.Base(filePath)).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", filePath, err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("rendering template %s: %w", filePath, err)
	}

	return nil
}
