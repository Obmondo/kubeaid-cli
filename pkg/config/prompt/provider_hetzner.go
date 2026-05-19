// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

type hetznerPrompter struct{}

func newHetznerProvider() *hetznerPrompter {
	return &hetznerPrompter{}
}

func (p *hetznerPrompter) SummaryLines(cfg *PromptedConfig) []string {
	var lines []string

	// hcloud / hybrid: HCloud sizing applies to the (HCloud) control
	// plane. Pure bare-metal mode has none of those — show server
	// IDs instead so the operator can verify the IDs they typed.
	if cfg.HetznerMode == constants.HetznerModeHCloud || cfg.HetznerMode == constants.HetznerModeHybrid {
		lines = append(lines,
			fmt.Sprintf("  Zone:          %s", cfg.HetznerHCloudZone),
			fmt.Sprintf("  Machine type:  %s", cfg.HetznerCPMachineType),
			fmt.Sprintf("  LB region:     %s", cfg.HetznerLBRegion),
			fmt.Sprintf("  CP replicas:   %s", cfg.HetznerCPReplicas),
		)
	}

	if cfg.HetznerMode == constants.HetznerModeBareMetal {
		lines = append(lines,
			fmt.Sprintf("  CP replicas:   %s", cfg.HetznerCPReplicas),
			fmt.Sprintf("  CP servers:    %s", formatServerSummary(cfg.HetznerBMCPServerIDs, cfg.HetznerBMServerPublicIPs)),
			fmt.Sprintf("  Worker group:  %s", cfg.HetznerBMNodeGroupName),
			fmt.Sprintf("  Worker hosts:  %s", formatServerSummary(cfg.HetznerBMNodeGroupServerIDs, cfg.HetznerBMServerPublicIPs)),
			fmt.Sprintf("  API endpoint:  %s%s", cfg.HetznerBMEndpointHost, failoverSuffix(cfg.HetznerBMEndpointIsFailoverIP)),
		)
	}

	lines = append(lines, fmt.Sprintf("  Mode:          %s", cfg.HetznerMode))
	return lines
}

// formatServerSummary renders a comma-separated list of server IDs,
// each optionally suffixed with the public IP the Robot lookup
// returned. Used in the bare-metal summary box so the operator can
// double-check IDs map to expected boxes before confirming.
func formatServerSummary(ids []string, ips map[string]string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if ip, ok := ips[id]; ok && ip != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", id, ip))
			continue
		}
		parts = append(parts, id)
	}
	return strings.Join(parts, ", ")
}

func failoverSuffix(isFailover bool) string {
	if isFailover {
		return " (Failover IP)"
	}
	return ""
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

	// HA toggle is meaningless for bare-metal — the CP replica count
	// is just however many dedicated servers the operator picks in
	// the add-loop below. Keep the toggle for hcloud / hybrid where
	// it does drive a `replicas:` integer for auto-scaled CP machines.
	haGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable high availability for the control plane?").
			Value(&haChoice),
	).WithHideFunc(func() bool {
		return cfg.HetznerMode == constants.HetznerModeBareMetal
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
		).Title("Hetzner credentials").Description("Step 3/4"),
		sshKeyGroup,
		haGroup,
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

	switch cfg.HetznerMode {
	case constants.HetznerModeHCloud, constants.HetznerModeHybrid:
		// Default zone, smallest machine type, and LB region matching the zone.
		cfg.HetznerCPReplicas = "1"
		if haChoice {
			cfg.HetznerCPReplicas = "3"
		}
		cfg.HetznerHCloudZone = "eu-central"
		cfg.HetznerCPMachineType = "cax21"
		cfg.HetznerRegion = "hel1"
		cfg.HetznerLBRegion = cfg.HetznerRegion
		return nil

	case constants.HetznerModeBareMetal:
		// Strip the hcloud defaults pre-set in ConfigFromPrompt so
		// the summary box and the rendered general.yaml don't carry
		// orphan hcloud fields. nodeGroups.bareMetal carries Region
		// indirectly via the chosen servers, so HetznerRegion stays
		// blank — kubeaid-cli reads it from Robot at bootstrap.
		// HetznerCPReplicas is set by runHetznerBareMetalForm based
		// on how many CP servers the operator added in the add-loop.
		cfg.HetznerHCloudZone = ""
		cfg.HetznerCPMachineType = ""
		cfg.HetznerRegion = ""
		cfg.HetznerLBRegion = ""

		if err := runHetznerBareMetalForm(cfg); err != nil {
			return fmt.Errorf("collecting Hetzner bare-metal config: %w", err)
		}
		return nil
	}

	return nil
}
