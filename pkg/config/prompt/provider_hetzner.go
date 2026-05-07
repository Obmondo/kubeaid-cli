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

func (p *hetznerPrompter) RunCredentialsForm(cfg *PromptedConfig, _ *autoDetectedConfig) error {
	// Pre-fill the SSH key path default.
	if cfg.HetznerSSHKeyPath == "" {
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
			huh.NewInput().
				Title("SSH private key file path:").
				Value(&cfg.HetznerSSHKeyPath).
				Validate(validateSSHKeyPath),
			huh.NewConfirm().
				Title("Enable high availability for the control plane?").
				Value(&haChoice),
		).Title("Hetzner credentials").Description("Step 3/4"),
		robotGroup,
	).Run()
	if err != nil {
		return err
	}

	// Derive the key pair name from the file path basename.
	cfg.HetznerSSHKeyName = strings.TrimSuffix(
		filepath.Base(cfg.HetznerSSHKeyPath),
		filepath.Ext(cfg.HetznerSSHKeyPath),
	)

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
