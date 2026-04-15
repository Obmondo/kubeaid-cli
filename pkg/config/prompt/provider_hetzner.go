// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"path/filepath"
	"strings"
)

type hetznerPrompter struct {
	baseProvider
}

func newHetznerProvider() *hetznerPrompter {
	p := &hetznerPrompter{}
	p.questionsFunc = p.promptHetznerQuestions
	return p
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

func (p *hetznerPrompter) promptHetznerQuestions(cfg *PromptedConfig) error {
	if err := selectOption(
		"Mode:", []string{"hcloud", "bare-metal", "hybrid"},
		"hcloud", &cfg.HetznerMode,
	); err != nil {
		return err
	}

	// Provider credentials come next, immediately after mode.
	if err := requiredPassword("Cloud API token:", &cfg.HetznerAPIToken); err != nil {
		return err
	}

	if cfg.HetznerMode == "bare-metal" || cfg.HetznerMode == "hybrid" {
		if err := requiredInput("Robot username:", &cfg.HetznerRobotUser); err != nil {
			return err
		}

		if err := requiredPassword("Robot password:", &cfg.HetznerRobotPassword); err != nil {
			return err
		}
	}

	if err := promptSSHPrivateKeyPath(&cfg.HetznerSSHKeyPath, "SSH private key file path:"); err != nil {
		return err
	}

	// Derive the key pair name from the file path basename (e.g. "/home/user/.ssh/id_ed25519" → "id_ed25519").
	cfg.HetznerSSHKeyName = strings.TrimSuffix(
		filepath.Base(cfg.HetznerSSHKeyPath),
		filepath.Ext(cfg.HetznerSSHKeyPath),
	)

	if cfg.HetznerMode == "hcloud" || cfg.HetznerMode == "hybrid" {
		// Default zone, smallest machine type, and LB region matching the zone.
		cfg.HetznerHCloudZone = "eu-central"
		cfg.HetznerCPMachineType = "cax21"
		cfg.HetznerRegion = "hel1"
		cfg.HetznerLBRegion = cfg.HetznerRegion

		replicas, err := promptHAControlPlane()
		if err != nil {
			return err
		}

		cfg.HetznerCPReplicas = replicas
	}

	return nil
}
