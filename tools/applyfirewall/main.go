// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Command applyfirewall reconciles the Hetzner Robot stateless firewall on the
// PUBLIC IP of bare-metal nodes, directly against the Robot API.
//
// It is standalone, operational tooling for clusters whose management cluster is
// gone (e.g. a pivoted, self-managing workload cluster): the Robot firewall is an
// infrastructure object, so this needs no kubeconfig, CAPI, or management cluster
// — only Robot web-service credentials and each node's main (public) IP.
//
// Control-plane and worker nodes get different rulesets (see
// docs/hetzner-bare-metal-network-surface.md):
//
//   - control-plane (CP_SERVER_IPS): SSH (ALLOW_SSH_FROM, else all), deny 6443
//     (apiserver via the NetBird operator), allow 80/443 + ALLOW_PUBLIC scoped to
//     FAILOVER_IP (the public ingress), ICMP, return traffic.
//   - worker (WORKER_SERVER_IPS): SSH (ALLOW_SSH_FROM, else all), ICMP, return —
//     nothing else public.
//
// SAFETY: with ALLOW_SSH_FROM set, public SSH is restricted to those sources, and
// the apiserver (6443) is always denied on the public IP — reach it over NetBird.
// Defaults to DRY-RUN; set APPLY=true to push. Reversible via the Robot UI/API.
//
// Usage:
//
//	ROBOT_USER=... ROBOT_PASSWORD=... \
//	CP_SERVER_IPS=1.2.3.4,1.2.3.5,1.2.3.6 \
//	WORKER_SERVER_IPS=1.2.3.7,1.2.3.8 \
//	[FAILOVER_IP=1.2.3.9] \
//	[ALLOW_SSH_FROM=203.0.113.4,203.0.113.0/24] \
//	[ALLOW_PUBLIC=5432/tcp] \
//	[APPLY=true] \
//	go run ./tools/applyfirewall
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// group pairs a set of server IPs with the ruleset to apply to them.
type group struct {
	role    string
	ips     []string
	ruleset hetzner.FirewallRuleset
}

func main() {
	if err := run(); err != nil {
		slog.Error("applyfirewall failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	robotUser := os.Getenv("ROBOT_USER")
	robotPassword := os.Getenv("ROBOT_PASSWORD")
	if robotUser == "" || robotPassword == "" {
		return fmt.Errorf("ROBOT_USER and ROBOT_PASSWORD must both be set")
	}

	cpIPs := splitCSV(os.Getenv("CP_SERVER_IPS"))
	workerIPs := splitCSV(os.Getenv("WORKER_SERVER_IPS"))
	if len(cpIPs) == 0 && len(workerIPs) == 0 {
		return fmt.Errorf("set CP_SERVER_IPS and/or WORKER_SERVER_IPS (comma-separated node main IPs)")
	}

	allowSSHFrom, err := parseIPList("ALLOW_SSH_FROM", os.Getenv("ALLOW_SSH_FROM"))
	if err != nil {
		return err
	}

	allowPublic, err := parseAllowPublic(os.Getenv("ALLOW_PUBLIC"))
	if err != nil {
		return err
	}

	failoverIP, err := parseFailoverIP(os.Getenv("FAILOVER_IP"))
	if err != nil {
		return err
	}

	apply := isTruthy(os.Getenv("APPLY"))

	groups := []group{
		{role: "control-plane", ips: cpIPs, ruleset: hetzner.ControlPlaneIngressRuleset(allowSSHFrom, allowPublic, failoverIP)},
		{role: "worker", ips: workerIPs, ruleset: hetzner.WorkerIngressRuleset(allowSSHFrom)},
	}

	printPlan(groups, allowSSHFrom, failoverIP, apply)

	if !apply {
		fmt.Println("\nDRY-RUN — no changes made. Re-run with APPLY=true to push the rulesets above.")
		return nil
	}

	ctx := context.Background()
	client := hetzner.NewRobotFirewallClient(robotUser, robotPassword)

	failures, total := 0, 0
	for _, g := range groups {
		for _, ip := range g.ips {
			total++
			if err := client.EnsureRobotFirewall(ctx, ip, g.ruleset); err != nil {
				slog.Error("failed to reconcile firewall",
					slog.String("role", g.role), slog.String("server-ip", ip), slog.Any("error", err))
				failures++
				continue
			}
			slog.Info("reconciled firewall", slog.String("role", g.role), slog.String("server-ip", ip))
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d server(s) failed; see errors above", failures, total)
	}
	fmt.Printf("\nDone — reconciled the firewall on %d server(s).\n", total)
	return nil
}

// splitCSV splits a comma-separated env value, trimming spaces and dropping empties.
func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// parseIPList parses a comma-separated list of IPv4 addresses/CIDRs, rejecting
// anything that is neither — hostnames are not accepted. name is used in errors.
func parseIPList(name, value string) ([]string, error) {
	sources := splitCSV(value)
	for _, source := range sources {
		if !isIPOrCIDR(source) {
			return nil, fmt.Errorf("invalid %s entry %q: must be an IP address or CIDR", name, source)
		}
	}
	return sources, nil
}

// parseFailoverIP validates FAILOVER_IP as an IPv4 address or CIDR (or empty).
// It is the public ingress IP the control-plane 80/443 + allowPublic rules are
// scoped to (Traefik points at it); empty leaves them unscoped (any dst).
func parseFailoverIP(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || isIPOrCIDR(value) {
		return value, nil
	}
	return "", fmt.Errorf("invalid FAILOVER_IP %q: must be an IP address or CIDR", value)
}

// isIPOrCIDR reports whether s is a bare IP address or a CIDR.
func isIPOrCIDR(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// parseAllowPublic parses ALLOW_PUBLIC ("port[/proto],...") into FirewallPorts.
// The protocol, when present, must be tcp or udp; omit it for any protocol.
func parseAllowPublic(value string) ([]config.FirewallPort, error) {
	var ports []config.FirewallPort
	for _, item := range splitCSV(value) {
		port, protocol, hasProtocol := strings.Cut(item, "/")
		port = strings.TrimSpace(port)
		if port == "" {
			return nil, fmt.Errorf("invalid ALLOW_PUBLIC entry %q: empty port", item)
		}

		firewallPort := config.FirewallPort{Port: port}
		if hasProtocol {
			protocol = strings.TrimSpace(protocol)
			if protocol != "tcp" && protocol != "udp" {
				return nil, fmt.Errorf("invalid ALLOW_PUBLIC entry %q: protocol must be tcp or udp", item)
			}
			firewallPort.Protocol = protocol
		}
		ports = append(ports, firewallPort)
	}
	return ports, nil
}

// isTruthy reports whether an env value means "yes".
func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

// printPlan prints the SSH posture and, per role, the target servers and the
// ruleset that will be (or, in dry-run, would be) applied.
func printPlan(groups []group, allowSSHFrom []string, failoverIP string, apply bool) {
	mode := "DRY-RUN"
	if apply {
		mode = "APPLY"
	}

	fmt.Printf("Hetzner Robot firewall — mode: %s\n", mode)
	if len(allowSSHFrom) == 0 {
		fmt.Println("SSH (22): allowed from ALL (ALLOW_SSH_FROM is empty).")
	} else {
		fmt.Printf("SSH (22): allowed only from %s; all other public SSH denied.\n", strings.Join(allowSSHFrom, ", "))
	}
	fmt.Println("apiserver (6443): denied on the public IP (reach it over the NetBird operator).")

	for _, g := range groups {
		if len(g.ips) == 0 {
			continue
		}
		fmt.Printf("\n=== %s (%d): %s ===\n", g.role, len(g.ips), strings.Join(g.ips, ", "))
		if g.role == "control-plane" {
			if failoverIP == "" {
				fmt.Println("(FAILOVER_IP not set — 80/443 + allowPublic open on any destination IP)")
			} else {
				fmt.Printf("(80/443 + allowPublic scoped to the failover IP %s)\n", failoverIP)
			}
		}
		printRuleset(g.ruleset)
	}
}

// printRuleset prints one ruleset as an aligned table.
func printRuleset(ruleset hetzner.FirewallRuleset) {
	fmt.Printf("ruleset (status=%s), evaluated top-to-bottom, first match wins:\n", ruleset.Status)
	fmt.Printf("  %-3s %-8s %-6s %-6s %-13s %-18s %-18s %s\n", "#", "ACTION", "IPVER", "PROTO", "DST_PORT", "SRC_IP", "DST_IP", "NAME")
	for i, rule := range ruleset.Rules {
		fmt.Printf("  %-3d %-8s %-6s %-6s %-13s %-18s %-18s %s\n",
			i, rule.Action, rule.IPVersion, orAny(rule.Protocol), orAny(rule.DstPort), orAny(rule.SrcIP), orAny(rule.DstIP), rule.Name)
	}
}

// orAny renders an empty firewall field as "any" for display.
func orAny(value string) string {
	if value == "" {
		return "any"
	}
	return value
}
