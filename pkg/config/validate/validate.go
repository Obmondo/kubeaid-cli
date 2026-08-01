// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package validate holds the field-level validators kubeaid-cli's
// interactive prompt runs at input time, exported so a consumer that
// isn't a terminal (the Obmondo add-cluster web form) can enforce the
// same rules — e.g. the Hetzner vSwitch VLAN ID range, the cluster name
// shape — before submitting.
//
// Every function here is pure (string in, error out) and free of
// pkg/config/prompt's charmbracelet/huh dependency, so importing this
// package does not pull a terminal-UI library into a server binary.
// pkg/config/prompt's own huh forms delegate to these exact functions.
package validate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	repourl "github.com/Obmondo/kubeaid-cli/pkg/repository/url"
)

// ErrRequired is returned by NonEmpty when the input is empty.
var ErrRequired = errors.New("value is required")

// NonEmpty rejects a blank (or whitespace-only) value.
func NonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return ErrRequired
	}
	return nil
}

// rfc1123Label matches a single DNS-1123 label — the shape Kubernetes
// requires of the CAPI Cluster object's name.
var rfc1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// maxClusterNameLen is the longest cluster name ClusterName accepts.
const maxClusterNameLen = 63

// ClusterName validates a cluster.name value: non-empty, no dots (the
// name is spliced into DNS labels — the NetBird peer FQDN k8s-<name>,
// HCloud/Robot resource names), at most 63 characters, and RFC-1123
// label shaped (CAPI's Cluster object name rule). Mirrors
// pkg/config/parser's validateClusterName.
func ClusterName(s string) error {
	if err := NonEmpty(s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)

	if strings.Contains(s, ".") {
		return errors.New("cluster name cannot contain dots — use '-' instead (e.g. kam-acme-com)")
	}

	if len(s) > maxClusterNameLen {
		return fmt.Errorf("cluster name must be at most %d characters", maxClusterNameLen)
	}

	if !rfc1123Label.MatchString(s) {
		return errors.New("cluster name must be lowercase alphanumeric or '-', and start and end with an alphanumeric character")
	}

	return nil
}

// SSHGitURL validates that s is a non-empty SSH-form Git URL (scp-like
// git@host:path or ssh://…) — HTTPS URLs are rejected.
func SSHGitURL(s string) error {
	if err := NonEmpty(s); err != nil {
		return err
	}
	if repourl.UsingHTTPBasedProtocol(strings.TrimSpace(s)) {
		return errors.New("must be SSH form (e.g. git@github.com:org/repo.git)")
	}
	return nil
}

// IPv4 validates that s is a non-empty IPv4 address.
func IPv4(s string) error {
	if err := NonEmpty(s); err != nil {
		return err
	}
	parsed := net.ParseIP(strings.TrimSpace(s))
	if parsed == nil || parsed.To4() == nil {
		return errors.New("must be a valid IPv4 address (e.g. 10.0.0.5)")
	}
	return nil
}

// CIDRv4 validates that s parses as an IPv4 CIDR (e.g. 10.0.1.0/24).
func CIDRv4(s string) error {
	if err := NonEmpty(s); err != nil {
		return err
	}
	_, _, err := net.ParseCIDR(strings.TrimSpace(s))
	if err != nil {
		return errors.New("must be a valid IPv4 CIDR (e.g. 10.0.1.0/24)")
	}
	return nil
}

// MinHetznerVLANID / MaxHetznerVLANID bound the VLAN IDs Hetzner's
// vSwitch webservice accepts (inclusive).
const (
	MinHetznerVLANID = 4000
	MaxHetznerVLANID = 4091
)

// HetznerVLANID validates a Hetzner vSwitch VLAN ID: numeric and within
// MinHetznerVLANID-MaxHetznerVLANID — the range Hetzner's webservice
// accepts.
func HetznerVLANID(s string) error {
	if err := NonEmpty(s); err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return errors.New("must be numeric")
	}
	if n < MinHetznerVLANID || n > MaxHetznerVLANID {
		return fmt.Errorf("hetzner vSwitch VLAN ID must be in %d-%d", MinHetznerVLANID, MaxHetznerVLANID)
	}
	return nil
}

// IPv4InSubnet returns a validator requiring its input to be a valid
// IPv4 address inside cidr. An empty cidr disables the containment
// check, falling back to a plain IPv4 validation.
func IPv4InSubnet(cidr string) func(string) error {
	return func(s string) error {
		if err := IPv4(s); err != nil {
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

// HTTPSURL validates that s is a non-empty https:// URL with a host.
func HTTPSURL(s string) error {
	if err := NonEmpty(s); err != nil {
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
