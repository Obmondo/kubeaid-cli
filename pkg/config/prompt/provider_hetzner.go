// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

type hetznerPrompter struct{}

const (
	defaultHetznerHCloudZone  = "eu-central"
	defaultHetznerMachineType = "cax21"
	defaultHetznerRegion      = "hel1"
)

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

	// Robot username + password masked (the username is a Robot web-
	// service identifier, typically distinct from the operator's
	// human account name — leaking it on-screen during a pairing /
	// screen-share offers no upside).
	//
	// Password's Validate hits Robot's GET /server once Enter is
	// pressed. On success the resulting inventory is cached on cfg
	// for the BM add-loop's autocomplete; on failure (401 / network)
	// the operator sees the error inline and stays on the field
	// instead of finding out later when the first server-ID lookup
	// fails.
	robotGroup := huh.NewGroup(
		huh.NewInput().
			Title("Robot username:").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.HetznerRobotUser).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Robot password:").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.HetznerRobotPassword).
			Validate(validateRobotCredentials(cfg)),
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

	// HA toggle drives the CP replica count across every Hetzner
	// mode. hcloud / hybrid scale identical machines, so 1 vs 3 is a
	// trivial knob there. For bare-metal it's still 1 or 3 — even
	// counts (2 / 4) lose etcd quorum, and 5+ dedicated CP servers is
	// rare enough that the prompt doesn't surface it (operator can
	// edit general.yaml after generation for unusual topologies).
	haGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable high availability for the control plane?").
			Description("Yes → 3 CP servers (recommended for production). No → 1 CP server.").
			Value(&haChoice),
	)

	// Cloud API token lives in its own group so it can be skipped
	// for pure bare-metal — that mode doesn't talk to HCloud at all
	// (no HCloud client, no NAT gateway, no LB). The group's
	// HideFunc reads cfg.HetznerMode, which is set by the Mode
	// selector in the group above and re-evaluated when the form
	// advances to this group.
	apiTokenGroup := huh.NewGroup(
		huh.NewInput().
			Title("Cloud API token:").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.HetznerAPIToken).
			Validate(nonEmpty),
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
		).Title("Hetzner credentials").Description("Step 3/4"),
		apiTokenGroup,
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
	case constants.HetznerModeHCloud:
		// Default zone, smallest machine type, and LB region matching the zone.
		cfg.HetznerCPReplicas = "1"
		if haChoice {
			cfg.HetznerCPReplicas = "3"
		}
		cfg.HetznerHCloudZone = defaultHetznerHCloudZone
		cfg.HetznerCPMachineType = defaultHetznerMachineType
		cfg.HetznerRegion = defaultHetznerRegion
		cfg.HetznerLBRegion = cfg.HetznerRegion
		return nil

	case constants.HetznerModeHybrid:
		// HCloud-side CP defaults — same shape as pure hcloud mode,
		// since the control plane lives in HCloud for hybrid.
		cfg.HetznerCPReplicas = "1"
		if haChoice {
			cfg.HetznerCPReplicas = "3"
		}
		cfg.HetznerHCloudZone = defaultHetznerHCloudZone
		cfg.HetznerCPMachineType = defaultHetznerMachineType
		cfg.HetznerRegion = defaultHetznerRegion
		cfg.HetznerLBRegion = cfg.HetznerRegion

		// Bare-metal worker node group + vSwitch. vSwitch is required:
		// CreateVSwitch in prerequisite_infrastructure.go runs
		// unconditionally for hybrid and panics on a nil block.
		if err := runHetznerHybridBareMetalForm(cfg); err != nil {
			return fmt.Errorf("collecting Hetzner hybrid bare-metal config: %w", err)
		}
		return nil

	case constants.HetznerModeBareMetal:
		// HA toggle pins the CP replica count to 1 (single) or 3
		// (quorum-safe) — the add-loop below collects exactly that
		// many servers without a per-iteration "add another?" prompt.
		cfg.HetznerCPReplicas = "1"
		if haChoice {
			cfg.HetznerCPReplicas = "3"
		}
		// Strip the hcloud defaults pre-set in ConfigFromPrompt so
		// the summary box and the rendered general.yaml don't carry
		// orphan hcloud fields. nodeGroups.bareMetal carries Region
		// indirectly via the chosen servers, so HetznerRegion stays
		// blank — kubeaid-cli reads it from Robot at bootstrap.
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
