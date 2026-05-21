// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// printSummary renders the configuration summary box.
func printSummary(cfg *PromptedConfig) {
	lines := []string{
		fmt.Sprintf("  Cluster:       %s (%s)", cfg.ClusterName, cfg.ClusterType),
		fmt.Sprintf("  K8s version:   %s (auto-detected)", cfg.K8sVersion),
		fmt.Sprintf("  KubeAid:       %s@%s", cfg.KubeaidForkURL, cfg.KubeaidVersion),
		fmt.Sprintf("  Config Repo:   %s", cfg.KubeaidConfigForkURL),
		fmt.Sprintf("  Cloud:         %s", cfg.CloudProvider),
	}

	prompter := prompterForProvider(cfg.CloudProvider)
	lines = append(lines, prompter.SummaryLines(cfg)...)

	if cfg.ClusterType == constants.ClusterTypeVPN {
		lines = append(lines,
			fmt.Sprintf("  Keycloak DNS:  %s", cfg.KeycloakDNS),
			fmt.Sprintf("  NetBird DNS:   %s", cfg.NetBirdDNS),
			fmt.Sprintf("  CP endpoint:   %s", cfg.ControlPlaneEndpoint),
			fmt.Sprintf("  ACME email:    %s", cfg.ACMEEmail),
		)
	}

	if cfg.CloudProvider != constants.CloudProviderLocal {
		gitAuth := "SSH key: " + cfg.SSHKeyPath
		if cfg.UseSSHAgent {
			gitAuth = "SSH agent (yubikey)"
		}

		lines = append(lines,
			fmt.Sprintf("  Deploy key:    %s", cfg.KubeaidConfigDeployKeyPath),
			fmt.Sprintf("  Git push:      %s", gitAuth),
		)
	}

	fmt.Println()
	printBox("Configuration Summary", lines)
}

// printBox renders lines inside a rounded-corner box with a title in the top border.
// Long lines are wrapped at the terminal width, with continuation lines indented
// to the value column (position of the first ':' + 2).
//
//	╭─ Title ──────────────────────────────────╮
//	│                                          │
//	│  Key:    short value                     │
//	│  Long:   git@dev.azure.com:v3/very-long- │
//	│          organization/project/repo.git   │
//	│                                          │
//	╰──────────────────────────────────────────╯
func printBox(title string, lines []string) {
	const defaultTermWidth = 80

	termWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth < 40 {
		termWidth = defaultTermWidth
	}

	// maxInner is the maximum content width that fits inside the box.
	// Each box line looks like: │<content padded to width><space>│
	// So total display columns = 1 (│) + width + 1 (space) + 1 (│) = width + 3.
	// We want width + 3 <= termWidth, so width <= termWidth - 3.
	maxInner := termWidth - 3

	// Wrap lines that exceed maxInner, indented to the value column.
	var wrapped []string
	for _, l := range lines {
		if runewidth.StringWidth(l) <= maxInner {
			wrapped = append(wrapped, l)
			continue
		}
		wrapped = append(wrapped, wrapLine(l, maxInner)...)
	}

	// Compute the box width: expand to fit the longest (wrapped) line,
	// but never exceed maxInner.
	minWidth := runewidth.StringWidth(title) + 4
	width := minWidth
	for _, l := range wrapped {
		if w := runewidth.StringWidth(l); w > width {
			width = w
		}
	}
	if width > maxInner {
		width = maxInner
	}

	// padRight pads s with trailing spaces so its display width equals targetWidth.
	padRight := func(s string, targetWidth int) string {
		gap := targetWidth - runewidth.StringWidth(s)
		if gap <= 0 {
			return s
		}
		return s + strings.Repeat(" ", gap)
	}

	// Top border: ╭─ Title ───...─╮
	topFill := width - runewidth.StringWidth(title) - 2
	if topFill < 1 {
		topFill = 1
	}
	fmt.Printf("╭─ %s %s╮\n", title, strings.Repeat("─", topFill))

	// Empty padding line.
	fmt.Printf("│%s│\n", padRight("", width+1))

	// Content lines.
	for _, l := range wrapped {
		fmt.Printf("│%s│\n", padRight(l, width+1))
	}

	// Empty padding line.
	fmt.Printf("│%s│\n", padRight("", width+1))

	// Bottom border: ╰───...─╯
	fmt.Printf("╰%s╯\n", strings.Repeat("─", width+1))
}

// wrapLine splits a line that exceeds maxWidth into multiple lines.
// Continuation lines are indented to the value column (position after ": ").
func wrapLine(line string, maxWidth int) []string {
	// Find the indent point: right after the first ":  " pattern (key: value alignment).
	indent := 0
	if idx := strings.Index(line, ":"); idx >= 0 {
		// Skip past the colon and any spaces that follow it.
		indent = idx + 1
		for indent < len(line) && line[indent] == ' ' {
			indent++
		}
	}

	// Safety: if the indent itself is too wide, fall back to the original leading whitespace.
	if indent >= maxWidth/2 {
		indent = len(line) - len(strings.TrimLeft(line, " "))
	}

	prefix := strings.Repeat(" ", indent)
	var result []string

	for runewidth.StringWidth(line) > maxWidth {
		// Walk runes until we fill maxWidth display columns.
		w := 0
		breakAt := 0
		for i, r := range line {
			rw := runewidth.RuneWidth(r)
			if w+rw > maxWidth {
				breakAt = i
				break
			}
			w += rw
			breakAt = i + len(string(r))
		}
		result = append(result, line[:breakAt])
		line = prefix + line[breakAt:]
	}

	result = append(result, line)
	return result
}

func validateSSHKeyPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errRequired
	}

	keyPath := expandTilde(p)

	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("file not found: %s", keyPath)
	}

	if err := validateSSHPrivateKey(data); err != nil {
		return fmt.Errorf("not a valid SSH private key: %w", err)
	}

	return nil
}

// validateSSHPrivateKey parses the given bytes as an SSH private key. Encrypted
// keys are considered valid — the passphrase will be supplied later.
func validateSSHPrivateKey(data []byte) error {
	if _, err := ssh.ParseRawPrivateKey(data); err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil
		}
		return err
	}
	return nil
}

// detectSSHKeyPath returns the path to the first SSH private key
// the operator has on this machine. Lookup order:
//
//  1. ~/.ssh/id_ed25519 then ~/.ssh/id_rsa (the standard names).
//  2. Whatever the SSH agent (yubikey or ssh-add'd key) reports
//     via `ssh-add -L`. Each line's 3rd field is typically the
//     original key file path the agent was given — match on a
//     real file under ~/.ssh and return that.
//
// Returns "" when nothing matches; caller's huh form falls back
// to an empty input the operator can fill in by hand.
func detectSSHKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	for _, p := range []string{
		path.Join(home, ".ssh", "id_ed25519"),
		path.Join(home, ".ssh", "id_rsa"),
	} {
		if isPrivateKeyFile(p) {
			return "~/.ssh/" + path.Base(p)
		}
	}

	// Standard names didn't hit. If the agent has identities
	// loaded (yubikey, or any ssh-add'd key), one of them is
	// almost certainly the key the operator wants for this
	// cluster — peek at the agent's public-key listing for path
	// hints.
	if p := detectAgentSSHKeyPath(home); p != "" {
		return p
	}

	return ""
}

// detectAgentSSHKeyPath dials SSH_AUTH_SOCK and asks the agent
// for its loaded identities. For each identity whose comment is
// an absolute file path under home, return the first one whose
// corresponding private-key file exists. Returns "" if no agent
// / no agent keys / no comment matches a real file (yubikey-only
// setups where the comment is the card serial fall here).
func detectAgentSSHKeyPath(home string) string {
	socketPath := os.Getenv(constants.EnvNameSSHAuthSock)
	if socketPath == "" {
		return ""
	}
	conn, err := net.Dial("unix", socketPath) //nolint:gosec // G704: dialing the operator's own SSH agent socket from $SSH_AUTH_SOCK.
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()

	identities, err := agent.NewClient(conn).List()
	if err != nil {
		return ""
	}
	for _, key := range identities {
		if !strings.HasPrefix(key.Comment, home+"/") {
			continue
		}
		if isPrivateKeyFile(key.Comment) {
			// Prefer the home-relative form since the rest of
			// the prompt collects ~/-style paths.
			return "~" + strings.TrimPrefix(key.Comment, home)
		}
	}
	return ""
}

// isPrivateKeyFile reports whether path exists and contains the
// "PRIVATE KEY" marker that's common to PEM and OpenSSH-format
// private keys.
func isPrivateKeyFile(p string) bool {
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "PRIVATE KEY")
}

// expandTilde resolves a leading ~ to the user's home directory.
func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return homeDir + p[1:]
}

// expandPaths resolves tilde (~) to the user's home directory in all file path fields.
func expandPaths(cfg *PromptedConfig) {
	cfg.SSHKeyPath = expandTilde(cfg.SSHKeyPath)
	cfg.KubeaidConfigDeployKeyPath = expandTilde(cfg.KubeaidConfigDeployKeyPath)
	cfg.HetznerSSHKeyPath = expandTilde(cfg.HetznerSSHKeyPath)
}
