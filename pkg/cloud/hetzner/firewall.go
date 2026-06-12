// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
)

// FirewallRule models a single rule in a Hetzner Robot stateless firewall.
//
// IPVersion must be "ipv4" or "ipv6".
// Protocol is "tcp", "udp", "icmp", or "" (any).
// DstPort is a port number or a "lo-hi" range string (e.g. "32768-65535"); "" for any.
// SrcIP and DstIP are CIDR strings; "" means any.
// Action is "accept" or "discard".
type FirewallRule struct {
	Name      string `json:"name"`
	IPVersion string `json:"ip_version"`
	Protocol  string `json:"protocol,omitempty"`
	DstPort   string `json:"dst_port,omitempty"`
	SrcIP     string `json:"src_ip,omitempty"`
	DstIP     string `json:"dst_ip,omitempty"`
	Action    string `json:"action"`
}

// FirewallRuleset is the complete set of rules for one direction on a
// Hetzner Robot per-server firewall.
//
// Status is "active" or "disabled".
// Rules are evaluated top-to-bottom; the first match wins.
type FirewallRuleset struct {
	Status string         `json:"status"`
	Rules  []FirewallRule `json:"rules"`
}

// robotFirewallResponse is the envelope returned by GET /firewall/<server-ip>.
type robotFirewallResponse struct {
	Firewall struct {
		ServerIP string `json:"server_ip"`
		Status   string `json:"status"`
		Rules    struct {
			Input []FirewallRule `json:"input"`
		} `json:"rules"`
	} `json:"firewall"`
}

// DefaultBareMetalIngressRuleset returns the operator-confirmed inbound ruleset
// for kubeaid bare-metal nodes (see docs/hetzner-bare-metal-network-surface.md,
// "Public IP — INBOUND" table). Evaluated top-to-bottom; unmatched traffic is
// dropped by the firewall's implicit default-deny.
//
// Rules:
//   - DENY 22/tcp   — SSH only via NetBird mesh
//   - DENY 6443/tcp — kube-apiserver only via NetBird mesh
//   - ALLOW 80/tcp  — Traefik public-web IngressClass
//   - ALLOW 443/tcp — Traefik public-websecure IngressClass
//   - ALLOW ICMP    — operational ping
//   - ALLOW 32768-65535/any — stateless return traffic; empty Protocol means any
//     protocol so UDP replies (DNS, NTP, NodePort UDP) are not silently dropped
func DefaultBareMetalIngressRuleset() FirewallRuleset {
	return FirewallRuleset{
		Status: "active",
		Rules: []FirewallRule{
			{
				Name:      "deny-ssh",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "22",
				Action:    "discard",
			},
			{
				Name:      "deny-kube-apiserver",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "6443",
				Action:    "discard",
			},
			{
				Name:      "allow-http",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "80",
				Action:    "accept",
			},
			{
				Name:      "allow-https",
				IPVersion: "ipv4",
				Protocol:  "tcp",
				DstPort:   "443",
				Action:    "accept",
			},
			{
				Name:      "allow-icmp",
				IPVersion: "ipv4",
				Protocol:  "icmp",
				Action:    "accept",
			},
			{
				Name:      "allow-return-traffic",
				IPVersion: "ipv4",
				// Protocol is intentionally empty (omitempty omits it from the POST body),
				// which Robot interprets as "any protocol". This is required so that UDP
				// reply packets (DNS, NTP, NodePort UDP) are not silently dropped — a
				// TCP-only rule would cover only TCP return traffic.
				DstPort: "32768-65535",
				Action:  "accept",
			},
		},
	}
}

// firewallRulesetEqual reports whether two FirewallRulesets are semantically
// identical.
//
// reflect.DeepEqual cannot be used directly because Robot returns JSON null for
// an empty rule list, which Unmarshal maps to a nil slice, whereas the desired
// ruleset is constructed with []FirewallRule{} (non-nil empty). DeepEqual
// returns false for nil vs empty slice even though both mean "zero rules".
// This helper normalises both cases so a reconcile loop does not issue a
// spurious POST on every iteration.
func firewallRulesetEqual(a, b FirewallRuleset) bool {
	if a.Status != b.Status {
		return false
	}
	if len(a.Rules) != len(b.Rules) {
		return false
	}
	for i := range a.Rules {
		if a.Rules[i] != b.Rules[i] {
			return false
		}
	}
	return true
}

// EnsureRobotFirewall idempotently reconciles the Hetzner Robot per-server
// stateless firewall for the given serverIP to the desired inbound ruleset.
//
// Flow:
//  1. GET /firewall/<serverIP> — fetch current state.
//  2. Compare current inbound rules + status against desired.
//  3. If equal, return nil (no-op).
//  4. Otherwise POST /firewall/<serverIP> with desired rules encoded in
//     Hetzner Robot bracket-notation form-data.
//
// The method is intentionally not wired into any provisioning phase. Timing
// (pre- vs post-NetBird) is an unresolved team decision; wiring lives in a
// follow-up PR.
func (h *Hetzner) EnsureRobotFirewall(ctx context.Context, serverIP string, desired FirewallRuleset) error {
	current, err := h.getRobotFirewall(serverIP)
	if err != nil {
		return fmt.Errorf("getting firewall for server %s: %w", serverIP, err)
	}

	if firewallRulesetEqual(current, desired) {
		slog.InfoContext(
			ctx, "Firewall already at desired state",
			slog.String("server-ip", serverIP),
		)
		return nil
	}

	if err := h.postRobotFirewall(ctx, serverIP, desired); err != nil {
		return fmt.Errorf("applying firewall for server %s: %w", serverIP, err)
	}

	return nil
}

// getRobotFirewall fetches the current firewall configuration for the given
// server IP from Hetzner Robot and returns it as a FirewallRuleset.
func (h *Hetzner) getRobotFirewall(serverIP string) (FirewallRuleset, error) {
	response, err := h.robotClient.R().Get("/firewall/" + serverIP)
	if err != nil {
		return FirewallRuleset{}, fmt.Errorf("requesting firewall details: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return FirewallRuleset{}, fmt.Errorf("unexpected status %d when getting firewall for server %s",
			response.StatusCode(), serverIP)
	}

	var body robotFirewallResponse
	if err := json.Unmarshal(response.Body(), &body); err != nil {
		return FirewallRuleset{}, fmt.Errorf("unmarshalling firewall response: %w", err)
	}

	return FirewallRuleset{
		Status: body.Firewall.Status,
		Rules:  body.Firewall.Rules.Input,
	}, nil
}

// postRobotFirewall sends a POST /firewall/<serverIP> with the desired
// ruleset encoded in Hetzner Robot's bracket-notation form-data shape.
//
// Robot expects each rule field as rules[input][N][field], e.g.:
//
//	rules[input][0][action]=accept
//	rules[input][0][ip_version]=ipv4
//
// See https://robot.your-server.de/doc/webservice/en.html#post-firewall-server-ip.
func (h *Hetzner) postRobotFirewall(ctx context.Context, serverIP string, desired FirewallRuleset) error {
	formValues := url.Values{
		"status": []string{desired.Status},
	}

	for i, rule := range desired.Rules {
		prefix := fmt.Sprintf("rules[input][%d]", i)
		formValues.Set(prefix+"[action]", rule.Action)
		formValues.Set(prefix+"[ip_version]", rule.IPVersion)
		formValues.Set(prefix+"[name]", rule.Name)

		// Omit optional fields when empty so Robot doesn't get a spurious
		// empty-string constraint.
		if rule.Protocol != "" {
			formValues.Set(prefix+"[protocol]", rule.Protocol)
		}
		if rule.DstPort != "" {
			formValues.Set(prefix+"[dst_port]", rule.DstPort)
		}
		if rule.SrcIP != "" {
			formValues.Set(prefix+"[src_ip]", rule.SrcIP)
		}
		if rule.DstIP != "" {
			formValues.Set(prefix+"[dst_ip]", rule.DstIP)
		}
	}

	response, err := h.robotClient.R().
		SetFormDataFromValues(formValues).
		Post("/firewall/" + serverIP)
	if err != nil {
		return fmt.Errorf("posting firewall rules: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected status %d when posting firewall rules", response.StatusCode())
	}

	slog.InfoContext(
		ctx, "Applied firewall ruleset",
		slog.String("server-ip", serverIP),
		slog.String("status", desired.Status),
		slog.Int("rules", len(desired.Rules)),
	)

	return nil
}
