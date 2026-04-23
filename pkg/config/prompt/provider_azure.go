// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"strings"
)

type azurePrompter struct {
	baseProvider
}

func newAzureProvider() *azurePrompter {
	p := &azurePrompter{}
	p.questionsFunc = p.promptAzureQuestions
	return p
}

func (p *azurePrompter) SummaryLines(cfg *PromptedConfig) []string {
	return []string{
		fmt.Sprintf("  Location:      %s", cfg.AzureLocation),
		fmt.Sprintf("  VM size:       %s", cfg.AzureCPVMSize),
		fmt.Sprintf("  Disk size:     %s GB", cfg.AzureCPDiskSizeGB),
		fmt.Sprintf("  CP replicas:   %s", cfg.AzureCPReplicas),
	}
}

func (p *azurePrompter) promptAzureQuestions(cfg *PromptedConfig) error {
	// Provider credentials come first, immediately after cluster name.
	if err := requiredInput("Tenant ID:", &cfg.AzureTenantID); err != nil {
		return err
	}

	if err := requiredInput("Subscription ID:", &cfg.AzureSubscriptionID); err != nil {
		return err
	}

	if err := requiredInput("Client ID:", &cfg.AzureClientID); err != nil {
		return err
	}

	if err := requiredPassword("Client Secret:", &cfg.AzureClientSecret); err != nil {
		return err
	}

	// Default location, smallest general-purpose VM, and disk size.
	cfg.AzureLocation = "westeurope"
	cfg.AzureCPVMSize = "Standard_B2s"
	cfg.AzureCPDiskSizeGB = "128"

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

	replicas, err := promptHAControlPlane()
	if err != nil {
		return err
	}

	cfg.AzureCPReplicas = replicas

	return nil
}
