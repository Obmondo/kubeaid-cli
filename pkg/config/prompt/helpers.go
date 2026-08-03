// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"
	"text/template"

	"golang.org/x/net/publicsuffix"

	"github.com/Obmondo/kubeaid-cli/pkg/config/validate"
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

// errRequired is prompt's name for validate.ErrRequired — the file-path
// validators in prompt_helper.go return it directly for a blank input,
// same as validate.NonEmpty does.
var errRequired = validate.ErrRequired

// minHetznerVLANID / maxHetznerVLANID are prompt's names for
// pkg/config/validate's exported VLAN ID bounds, kept for the vSwitch
// add-loop's "next free VLAN ID" scan in provider_hetzner_baremetal.go.
const (
	minHetznerVLANID = validate.MinHetznerVLANID
	maxHetznerVLANID = validate.MaxHetznerVLANID
)

// nonEmpty / clusterName / sshGitURL / ipv4 / cidrv4 / hetznerVLANID /
// ipv4InSubnet / httpsURL are prompt's names for pkg/config/validate's
// exported field validators, kept so every huh Validate(...) call site in
// this package is unchanged. See that package for the validation rules —
// the Obmondo API imports it directly to enforce the same rules in the
// browser.
var (
	nonEmpty      = validate.NonEmpty
	clusterName   = validate.ClusterName
	sshGitURL     = validate.SSHGitURL
	ipv4          = validate.IPv4
	cidrv4        = validate.CIDRv4
	hetznerVLANID = validate.HetznerVLANID
	ipv4InSubnet  = validate.IPv4InSubnet
	httpsURL      = validate.HTTPSURL
)

// renderTemplate executes a Go template string against data and returns the
// rendered bytes. name identifies the template in parse/execution error
// messages. Pure — no filesystem or network access.
func renderTemplate(name string, tmplStr string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", name, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return nil, fmt.Errorf("rendering template %s: %w", name, err)
	}

	return rendered.Bytes(), nil
}

// writeTemplatedFile renders a Go template string with the given data and writes it to disk.
func writeTemplatedFile(filePath string, tmplStr string, data any, perm os.FileMode) error {
	rendered, err := renderTemplate(filePath, tmplStr, data)
	if err != nil {
		return err
	}

	return writeFile(filePath, rendered, perm)
}

// writeFile writes data to filePath with perm, creating parent directories
// as needed.
func writeFile(filePath string, data []byte, perm os.FileMode) error {
	dir := path.Dir(filePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	if err := os.WriteFile(filePath, data, perm); err != nil {
		return fmt.Errorf("creating file %s: %w", filePath, err)
	}

	return nil
}
