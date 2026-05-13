// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

type azurePrompter struct{}

func newAzureProvider() *azurePrompter {
	return &azurePrompter{}
}

func (p *azurePrompter) SummaryLines(cfg *PromptedConfig) []string {
	return []string{
		fmt.Sprintf("  Location:      %s", cfg.AzureLocation),
		fmt.Sprintf("  VM size:       %s", cfg.AzureCPVMSize),
		fmt.Sprintf("  Disk size:     %s GB", cfg.AzureCPDiskSizeGB),
		fmt.Sprintf("  CP replicas:   %s", cfg.AzureCPReplicas),
	}
}

func (p *azurePrompter) RunCredentialsForm(cfg *PromptedConfig, _ *autoDetectedConfig) error {
	// Default location, smallest general-purpose VM, and disk size.
	if cfg.AzureLocation == "" {
		cfg.AzureLocation = "westeurope"
	}
	if cfg.AzureCPVMSize == "" {
		cfg.AzureCPVMSize = "Standard_B2s"
	}
	if cfg.AzureCPDiskSizeGB == "" {
		cfg.AzureCPDiskSizeGB = "128"
	}

	haChoice := cfg.AzureCPReplicas != "1"

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Tenant ID:").
				Value(&cfg.AzureTenantID).
				Validate(nonEmpty),
			huh.NewInput().
				Title("Subscription ID:").
				Value(&cfg.AzureSubscriptionID).
				Validate(nonEmpty),
			huh.NewInput().
				Title("Client ID:").
				Value(&cfg.AzureClientID).
				Validate(nonEmpty),
			huh.NewInput().
				Title("Client Secret:").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.AzureClientSecret).
				Validate(nonEmpty),
			huh.NewConfirm().
				Title("Enable high availability for the control plane?").
				Value(&haChoice),
		).Title("Azure credentials").Description("Step 3/4"),
	).Run()
	if err != nil {
		return err
	}

	if haChoice {
		cfg.AzureCPReplicas = "3"
	} else {
		cfg.AzureCPReplicas = "1"
	}

	// Auto-generate storage account name from cluster name.
	// Azure requires: 3-24 chars, lowercase alphanumeric only.
	var sb strings.Builder
	for _, r := range strings.ToLower(cfg.ClusterName) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	name := sb.String() + "sa"
	if len(name) > 24 {
		name = name[:24]
	}
	cfg.AzureStorageAccount = name

	return nil
}
