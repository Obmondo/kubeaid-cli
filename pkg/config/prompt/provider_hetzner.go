// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
)

type hetznerPrompter struct{}

func newHetznerProvider() *hetznerPrompter {
	return &hetznerPrompter{}
}

func (p *hetznerPrompter) SummaryLines(cfg *PromptedConfig) []string {
	var lines []string
	if cfg.HetznerHCloudZone != "" {
		lines = append(lines,
			fmt.Sprintf("  Zone:          %s", cfg.HetznerHCloudZone),
			fmt.Sprintf("  Machine type:  %s", cfg.HetznerCPMachineType),
			fmt.Sprintf("  LB region:     %s", cfg.HetznerLBRegion),
			fmt.Sprintf("  CP replicas:   %s", cfg.HetznerCPReplicas),
		)
	}
	lines = append(lines, fmt.Sprintf("  Mode:          %s", cfg.HetznerMode))
	return lines
}

func (p *hetznerPrompter) RunCredentialsForm(cfg *PromptedConfig, detected *autoDetectedConfig) error {
	// Only pre-fill a file path default when we'll actually ask for
	// one. With an SSH agent reachable, the path stays empty and the
	// hetzner block of general.yaml renders useSSHAgent: true instead.
	if !detected.SSHAgentAvail && cfg.HetznerSSHKeyPath == "" {
		cfg.HetznerSSHKeyPath = detectSSHKeyPath()
	}

	// HA selector: 3 replicas for HA, 1 otherwise.
	haChoice := cfg.HetznerCPReplicas != "1"

	robotGroup := huh.NewGroup(
		huh.NewInput().
			Title("Robot username:").
			Value(&cfg.HetznerRobotUser).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Robot password:").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.HetznerRobotPassword).
			Validate(nonEmpty),
	).WithHideFunc(func() bool {
		return cfg.HetznerMode != "bare-metal" && cfg.HetznerMode != "hybrid"
	})

	// SSH key path is sourced from the agent when one is reachable
	// (yubikey or ssh-add'd key — see autodetect.detectSSHAgent).
	// Skip the prompt in that case so the operator isn't asked for
	// a path they don't have on disk.
	sshKeyGroup := huh.NewGroup(
		huh.NewInput().
			Title("SSH private key file path:").
			Value(&cfg.HetznerSSHKeyPath).
			Validate(validateSSHKeyPath),
	).WithHide(detected.SSHAgentAvail)

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Mode:").
				Options(
					huh.NewOption("hcloud", "hcloud"),
					huh.NewOption("bare-metal", "bare-metal"),
					huh.NewOption("hybrid", "hybrid"),
				).
				Value(&cfg.HetznerMode),
			huh.NewInput().
				Title("Cloud API token:").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.HetznerAPIToken).
				Validate(nonEmpty),
		).Title("Hetzner credentials").Description("Step 3/4"),
		sshKeyGroup,
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable high availability for the control plane?").
				Value(&haChoice),
		),
		robotGroup,
	).Run()
	if err != nil {
		return err
	}

	// Derive the HCloud SSH key resource name. With a file path, use
	// its basename (matches the operator's "this is my id_ed25519
	// key" mental model). With the agent (no file), fall back to the
	// cluster name — the resource name only needs to be unique per
	// HCloud account, and HCloud's idempotency check goes by
	// fingerprint anyway.
	if detected.SSHAgentAvail {
		cfg.HetznerSSHKeyName = cfg.ClusterName
	} else {
		cfg.HetznerSSHKeyName = strings.TrimSuffix(
			filepath.Base(cfg.HetznerSSHKeyPath),
			filepath.Ext(cfg.HetznerSSHKeyPath),
		)
	}

	if haChoice {
		cfg.HetznerCPReplicas = "3"
	} else {
		cfg.HetznerCPReplicas = "1"
	}

	if cfg.HetznerMode == "hcloud" || cfg.HetznerMode == "hybrid" {
		// Default zone, smallest machine type, and LB region matching the zone.
		cfg.HetznerHCloudZone = "eu-central"
		cfg.HetznerCPMachineType = "cax21"
		cfg.HetznerRegion = "hel1"
		cfg.HetznerLBRegion = cfg.HetznerRegion
	}

	return nil
}
