// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// errKeyCaptured is a sentinel returned from the host-key callback
// once we've recorded the key — short-circuits ssh.Dial before it
// reaches authentication (which would otherwise fail or, with an
// SSH agent loaded, prompt for a YubiKey touch on what should be a
// no-op probe).
var errKeyCaptured = errors.New("host key captured")

// scanSSHHostKey opens a TCP/SSH handshake to host:port, captures
// the server's offered host key, and formats it as a known_hosts
// line. Pure-Go equivalent of `ssh-keyscan -p <port> <host>`.
//
// Returns the formatted line ready to drop into a known_hosts file
// or YAML's git.knownHosts list. Returns an error when the TCP
// dial or SSH handshake itself fails — host unreachable, port
// closed, etc. — so the prompt can warn the operator and leave
// git.knownHosts empty for them to populate by hand later.
func scanSSHHostKey(host string, port int) (string, error) {
	var captured ssh.PublicKey
	cfg := &ssh.ClientConfig{
		// User is required by ssh.Dial but never used — handshake
		// aborts in HostKeyCallback before auth runs.
		User: "git",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			return errKeyCaptured
		},
		Timeout: 10 * time.Second,
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := ssh.Dial("tcp", addr, cfg)
	if conn != nil {
		_ = conn.Close()
	}
	// If we captured the key, the error is our own sentinel
	// (wrapped by the ssh stack) — ignore it and report success.
	if captured != nil {
		return formatKnownHostsLine(host, port, captured), nil
	}
	if err != nil {
		return "", fmt.Errorf("ssh handshake to %s: %w", addr, err)
	}
	return "", fmt.Errorf("no host key captured from %s", addr)
}

// formatKnownHostsLine renders a single known_hosts-style entry.
// Default port (22) → bare hostname; non-default port → "[host]:port"
// per the OpenSSH known_hosts(5) escaping rules.
func formatKnownHostsLine(host string, port int, key ssh.PublicKey) string {
	hostField := host
	if port != 22 {
		hostField = fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s %s",
		hostField,
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
	)
}

// commonGitHosts is the set of hosts whose SSH server keys are
// already shipped in the embedded pkg/utils/git/templates/known_hosts
// (sourced from ArgoCD's argocd-ssh-known-hosts-cm). The prompt
// skips keyscan for these — the embedded entries cover them and a
// fresh scan would just produce a duplicate.
var commonGitHosts = map[string]bool{
	"github.com":        true,
	"gitlab.com":        true,
	"ssh.dev.azure.com": true,
	"bitbucket.org":     true,
}

// populateGitKnownHosts walks the kubeaid + kubeaid-config fork
// URLs the operator just supplied, runs ssh-keyscan on any
// SSH-form URL whose host isn't already covered by the embedded
// known_hosts, and appends the captured lines to cfg.GitKnownHosts
// for general.yaml.tmpl to render under git.knownHosts.
//
// Failures are non-fatal — printed as a warning so the operator
// can hand-populate git.knownHosts after the fact when the host
// is unreachable at prompt time.
func populateGitKnownHosts(cfg *PromptedConfig) {
	candidates := []string{
		cfg.KubeaidForkURL,
		cfg.KubeaidConfigForkURL,
	}

	seen := map[string]bool{}
	for _, raw := range candidates {
		host, port, err := parseHostPortFromGitURL(raw)
		if err != nil || host == "" {
			// HTTPS URL, empty, or unparseable — skip silently.
			// HTTPS doesn't need known_hosts; unparseable inputs
			// would have been caught by sshGitURL upstream.
			continue
		}
		if commonGitHosts[host] {
			continue
		}
		dedupeKey := fmt.Sprintf("%s:%d", host, port)
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true

		line, err := scanSSHHostKey(host, port)
		if err != nil {
			fmt.Printf("  ⚠ Could not fetch SSH host key for %s: %v\n", dedupeKey, err)
			fmt.Printf("    Add the line manually to git.knownHosts in general.yaml.\n")
			continue
		}
		cfg.GitKnownHosts = append(cfg.GitKnownHosts, line)
		fmt.Printf("  Captured SSH host key for %s\n", dedupeKey)
	}
}

// parseHostPortFromGitURL extracts host + port from a Git URL.
// Returns ("", 0, nil) for HTTPS URLs (no SSH host to scan).
func parseHostPortFromGitURL(rawURL string) (host string, port int, err error) {
	rawURL = strings.TrimSpace(rawURL)
	port = 22

	// scheme://[user@]host[:port]/...
	if strings.Contains(rawURL, "://") {
		if strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "http://") {
			return "", 0, nil
		}
		// strip scheme
		afterScheme := rawURL[strings.Index(rawURL, "://")+3:]
		// strip user@
		if at := strings.Index(afterScheme, "@"); at >= 0 {
			afterScheme = afterScheme[at+1:]
		}
		// take everything before first /
		hostPort := afterScheme
		if slash := strings.Index(afterScheme, "/"); slash >= 0 {
			hostPort = afterScheme[:slash]
		}
		if h, p, err := net.SplitHostPort(hostPort); err == nil {
			pn, _ := strconv.Atoi(p)
			return h, pn, nil
		}
		return hostPort, 22, nil
	}

	// scp-style: [user@]host:path  (always SSH, default port)
	rest := rawURL
	if at := strings.Index(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	if colon := strings.Index(rest, ":"); colon >= 0 {
		return rest[:colon], 22, nil
	}
	return "", 0, fmt.Errorf("unrecognized git URL form: %q", rawURL)
}
