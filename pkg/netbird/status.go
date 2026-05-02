// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

// Package netbird provides a small, read-only view of the local NetBird
// daemon's state. It shells out to `netbird status --json` rather than
// hitting the NetBird Management API, so callers don't need to handle
// API tokens — the daemon is already authenticated via `netbird up`.
package netbird

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// DaemonStatusConnected is the value of Status.DaemonStatus when the
	// local daemon is logged in and connected to the management server.
	DaemonStatusConnected = "Connected"
	// PeerStatusConnected is the value of Peer.Status when a peer has an
	// active mesh connection. We treat any other value as "not currently
	// reachable".
	PeerStatusConnected = "Connected"
)

// Status is the shape of `netbird status --json` (the subset we use). The
// daemon emits many more fields; we deliberately ignore them so a future
// NetBird release adding fields doesn't break our parse.
type Status struct {
	DaemonStatus string         `json:"daemonStatus"`
	Management   ManagementInfo `json:"management"`
	Peers        PeersInfo      `json:"peers"`
}

// ManagementInfo describes the daemon's view of the NetBird management
// server.
type ManagementInfo struct {
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// PeersInfo describes the peers known to the daemon.
type PeersInfo struct {
	Total     int    `json:"total"`
	Connected int    `json:"connected"`
	Details   []Peer `json:"details"`
}

// Peer is one mesh peer entry.
type Peer struct {
	FQDN   string `json:"fqdn"`
	Status string `json:"status"`
}

// runNetbirdStatus is a package-level variable so tests can stub the
// shell-out without coupling them to a real netbird binary.
var runNetbirdStatus = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "netbird", "status", "--json")
	return cmd.Output()
}

// FetchStatus runs `netbird status --json` and parses the output.
func FetchStatus(ctx context.Context) (*Status, error) {
	out, err := runNetbirdStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("running netbird status: %w", err)
	}

	var s Status
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("parsing netbird status JSON: %w", err)
	}

	return &s, nil
}

// AccessibleClusters returns the cluster names from peers whose FQDN
// matches `<prefix><name><suffix>` and whose Status is Connected. The
// returned slice contains just the `<name>` portion — the prefix and
// suffix are stripped.
//
// Example: prefix="k8s-", suffix=".netbird.selfhosted", a peer with
// fqdn "k8s-staging.netbird.selfhosted" produces "staging".
//
// Peers that don't match the prefix/suffix or aren't Connected are
// filtered out. Returns an empty slice (never nil) so callers can
// iterate uniformly.
func AccessibleClusters(s *Status, prefix, suffix string) []string {
	out := []string{}

	for _, p := range s.Peers.Details {
		if p.Status != PeerStatusConnected {
			continue
		}

		if !strings.HasPrefix(p.FQDN, prefix) || !strings.HasSuffix(p.FQDN, suffix) {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(p.FQDN, prefix), suffix)
		if name == "" {
			continue
		}

		out = append(out, name)
	}

	return out
}
