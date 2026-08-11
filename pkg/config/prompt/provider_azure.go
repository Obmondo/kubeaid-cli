// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

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
	if cfg.AzureAKS {
		return []string{
			fmt.Sprintf("  Location:      %s", cfg.AzureLocation),
			"  Control plane: AKS (managed by Azure)",
		}
	}
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

	// Control-plane flavour comes BEFORE credentials : the credentials are the
	// same either way, but every question after them differs — AKS has no HA /
	// replica choice (Azure runs the control plane) and no storage account
	// (it only hosts the self-managed workload-identity OIDC provider).
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[bool]().
				Title("Control plane:").
				Options(
					huh.NewOption("Self-managed (VMs, kubeadm)", false),
					huh.NewOption("AKS (managed by Azure)", true),
				).
				Value(&cfg.AzureAKS),
		).Title("Azure control plane").Description("Step 3/4"),
	).Run(); err != nil {
		return err
	}

	haChoice := cfg.AzureCPReplicas != "1"

	credGroup := huh.NewGroup(
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
	)

	haGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable high availability for the control plane?").
			Value(&haChoice),
	).WithHideFunc(func() bool {
		// AKS control planes are Azure-managed — nothing to ask.
		return cfg.AzureAKS
	})

	err := huh.NewForm(
		credGroup.Title("Azure credentials").Description("Step 3/4 (cont.)"),
		haGroup,
	).Run()
	if err != nil {
		return err
	}

	if cfg.AzureAKS {
		// Azure owns the control plane, and there is no storage account to
		// derive — nothing more to collect.
		return nil
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
