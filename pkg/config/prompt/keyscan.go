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

	"github.com/Obmondo/kubeaid-cli/pkg/utils/giturl"
)

// errKeyCaptured is a sentinel returned from the host-key callback
// once we've recorded the key — short-circuits ssh.Dial before it
// reaches authentication (which would otherwise fail or, with an
// SSH agent loaded, prompt for a YubiKey touch on what should be a
// no-op probe).
var errKeyCaptured = errors.New("host key captured")

// scanSSHHostKeyFunc is the production host-key scanner. Tests may
// replace it to avoid live network calls.
var scanSSHHostKeyFunc = scanSSHHostKey

// defaultSSHPorts maps forge hostnames to their SSH port when the
// Git URL is scp-style (git@host:path) and carries no explicit port.
// Self-hosted forges that listen on a non-default SSH port must be
// listed here so keyscan targets the daemon ArgoCD will actually use.
var defaultSSHPorts = map[string]int{
	"gitea.obmondo.com": 2223,
}

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
	return fmt.Sprintf(
		"%s %s",
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

	seen := knownHostScanKeysFromEntries(cfg.GitKnownHosts)
	for _, raw := range candidates {
		host, port, err := hostPortFromGitURL(raw)
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

		line, err := scanSSHHostKeyFunc(host, port)
		if err != nil {
			fmt.Printf("  ⚠ Could not fetch SSH host key for %s: %v\n", dedupeKey, err)
			fmt.Printf("    Add the line manually to git.knownHosts in general.yaml.\n")
			continue
		}
		cfg.GitKnownHosts = replaceKnownHostsForHostname(cfg.GitKnownHosts, host)
		cfg.GitKnownHosts = appendUniqueKnownHost(cfg.GitKnownHosts, line)
		fmt.Printf("  Captured SSH host key for %s\n", dedupeKey)
	}
}

// hostPortFromGitURL extracts host + port from a Git URL.
// Returns ("", 0, nil) for HTTPS URLs (no SSH host to scan).
func hostPortFromGitURL(rawURL string) (host string, port int, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if giturl.IsHTTP(rawURL) {
		return "", 0, nil
	}
	if !giturl.IsSSH(rawURL) {
		return "", 0, fmt.Errorf("unrecognized git URL form: %q", rawURL)
	}

	parsed, err := giturl.Parse(rawURL)
	if err != nil {
		return "", 0, err
	}

	host, port = splitHostPort(parsed.Host)
	if port == 22 {
		if override, ok := defaultSSHPorts[host]; ok {
			port = override
		}
	}
	return host, port, nil
}

func splitHostPort(hostPort string) (host string, port int) {
	if h, portStr, err := net.SplitHostPort(hostPort); err == nil {
		p, convErr := strconv.Atoi(portStr)
		if convErr != nil || p == 0 {
			return h, 22
		}
		return h, p
	}
	return hostPort, 22
}

func knownHostScanKeysFromEntries(entries []string) map[string]bool {
	seen := map[string]bool{}
	for _, line := range entries {
		host, port, ok := hostPortFromKnownHostsLine(line)
		if !ok {
			continue
		}
		seen[fmt.Sprintf("%s:%d", host, port)] = true
	}
	return seen
}

func hostPortFromKnownHostsLine(line string) (host string, port int, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}

	hostField := fields[0]
	if strings.HasPrefix(hostField, "[") {
		trimmed := strings.TrimPrefix(hostField, "[")
		hostPart, portPart, found := strings.Cut(trimmed, "]:")
		if !found || hostPart == "" || portPart == "" {
			return "", 0, false
		}
		parsedPort, err := strconv.Atoi(portPart)
		if err != nil {
			return "", 0, false
		}
		return hostPart, parsedPort, true
	}

	return hostField, 22, true
}

func replaceKnownHostsForHostname(entries []string, hostname string) []string {
	if hostname == "" {
		return entries
	}
	out := make([]string, 0, len(entries))
	for _, line := range entries {
		host, _, ok := hostPortFromKnownHostsLine(line)
		if ok && host == hostname {
			continue
		}
		out = append(out, line)
	}
	return out
}

func appendUniqueKnownHost(entries []string, line string) []string {
	for _, existing := range entries {
		if existing == line {
			return entries
		}
	}
	return append(entries, line)
}
