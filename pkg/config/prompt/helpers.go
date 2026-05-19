// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"

	"golang.org/x/net/publicsuffix"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
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

// sshGitURL validates that s is a non-empty SSH-form Git URL.
// Accepts the two common SSH forms (scp-like git@host:path and
// rfc-3986 ssh://...) and rejects http(s):// — used in the
// "private SSH KubeAid fork" path where HTTPS would defeat the
// reason for asking the question.
func sshGitURL(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}
	if giturl.IsHTTP(strings.TrimSpace(s)) {
		return errors.New("must be SSH form (e.g. git@github.com:org/repo.git)")
	}
	return nil
}

// ipv4 validates that s is a non-empty IPv4 address.
// Used for Hetzner bare-metal control-plane host private IPs and the
// API server endpoint host, both of which must resolve at parse time
// (validator tags `ipv4` / `ip`).
func ipv4(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}
	parsed := net.ParseIP(strings.TrimSpace(s))
	if parsed == nil || parsed.To4() == nil {
		return errors.New("must be a valid IPv4 address (e.g. 10.0.0.5)")
	}
	return nil
}

// cidrv4 validates that s parses as an IPv4 CIDR (e.g. 10.0.1.0/24).
// Used for the Hetzner vSwitch subnet block — server private IPs
// must live within this range, and Hetzner rejects malformed CIDRs.
func cidrv4(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}
	_, _, err := net.ParseCIDR(strings.TrimSpace(s))
	if err != nil {
		return errors.New("must be a valid IPv4 CIDR (e.g. 10.0.1.0/24)")
	}
	return nil
}

// hetznerVLANID validates a Hetzner vSwitch VLAN ID — the webservice
// only accepts 4000-4091 (inclusive). Anything outside is rejected at
// prompt time so the operator catches a typo before bootstrap.
func hetznerVLANID(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return errors.New("must be numeric")
	}
	const minVLAN, maxVLAN = 4000, 4091
	if n < minVLAN || n > maxVLAN {
		return fmt.Errorf("hetzner vSwitch VLAN ID must be in %d-%d", minVLAN, maxVLAN)
	}
	return nil
}

// ipv4InSubnet returns a validator that requires s to be a valid
// IPv4 inside cidr. cidr is captured at validator-build time; an
// empty cidr disables the containment check (validator falls back to
// plain ipv4) so the prompt still works when vSwitch wasn't asked
// for (pure-hcloud mode).
func ipv4InSubnet(cidr string) func(string) error {
	return func(s string) error {
		if err := ipv4(s); err != nil {
			return err
		}
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			return nil
		}
		_, subnet, err := net.ParseCIDR(cidr)
		if err != nil || subnet == nil {
			return nil
		}
		if !subnet.Contains(net.ParseIP(strings.TrimSpace(s))) {
			return fmt.Errorf("must be inside the vSwitch subnet %s", cidr)
		}
		return nil
	}
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
