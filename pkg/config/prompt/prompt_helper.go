// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-runewidth"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// printSectionHeader prints a section title inside a rounded rectangle.
//
//	╭───────────────────────────────╮
//	│   Cluster Configuration       │
//	╰───────────────────────────────╯
func printSectionHeader(title string) {
	fmt.Println()
	padding := 3
	inner := len(title) + padding*2 // equal padding on both sides
	fmt.Printf("  ╭%s╮\n", strings.Repeat("─", inner))
	pad := strings.Repeat(" ", padding)
	fmt.Printf("  │%s%s%s│\n", pad, title, pad)
	fmt.Printf("  ╰%s╯\n", strings.Repeat("─", inner))
	fmt.Println()
}

// printSummaryAndConfirm renders the configuration summary box and asks for confirmation.
func printSummaryAndConfirm(cfg *PromptedConfig) error {
	lines := []string{
		fmt.Sprintf("  Cluster:       %s (%s)", cfg.ClusterName, cfg.ClusterType),
		fmt.Sprintf("  K8s version:   %s (auto-detected)", cfg.K8sVersion),
		fmt.Sprintf("  KubeAid:       %s@%s", cfg.KubeaidForkURL, cfg.KubeaidVersion),
		fmt.Sprintf("  Config Repo:   %s", cfg.KubeaidConfigForkURL),
		fmt.Sprintf("  Cloud:         %s", cfg.CloudProvider),
	}

	prompter := prompterForProvider(cfg.CloudProvider)
	lines = append(lines, prompter.SummaryLines(cfg)...)

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

	var confirmed bool
	if err := confirm("Looks good?", true, &confirmed); err != nil {
		return err
	}

	if !confirmed {
		return fmt.Errorf("configuration not confirmed by user")
	}

	return nil
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

// promptSSHPrivateKeyPath asks for an SSH private key file path and validates that the
// file exists and looks like a PEM-encoded private key. Validation errors are shown
// inline by huh, which keeps the user on the prompt until a valid path is entered.
// If a well-known SSH key is found (~/.ssh/id_ed25519 or ~/.ssh/id_rsa), it is offered
// as the default.
func promptSSHPrivateKeyPath(dest *string, message string) error {
	*dest = detectSSHKeyPath()

	if err := huh.NewInput().
		Title(message).
		Value(dest).
		Validate(validateSSHKeyPath).
		Run(); err != nil {
		return err
	}

	printRecap(message, *dest)
	return nil
}

func validateSSHKeyPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errRequired
	}

	keyPath := expandTilde(path)

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

// detectSSHKeyPath returns the path to the first well-known SSH private key found,
// preferring ed25519 over RSA. Returns "" if none is found.
func detectSSHKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		path.Join(home, ".ssh", "id_ed25519"),
		path.Join(home, ".ssh", "id_rsa"),
	}

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "PRIVATE KEY") {
			return "~/.ssh/" + path.Base(p)
		}
	}

	return ""
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
