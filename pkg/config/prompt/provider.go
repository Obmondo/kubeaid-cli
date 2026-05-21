// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// ProviderPrompter collects provider-specific credentials and config.
type ProviderPrompter interface {
	// RunCredentialsForm shows the provider-specific Step 3 form and
	// populates provider credential fields on cfg. It also handles the
	// SSH auth form (Step 4 precursor) for providers that need it.
	RunCredentialsForm(cfg *PromptedConfig, detected *autoDetectedConfig) error

	// SummaryLines returns provider-specific lines for the summary box.
	SummaryLines(cfg *PromptedConfig) []string
}

func prompterForProvider(provider string) ProviderPrompter {
	switch provider {
	case constants.CloudProviderAWS:
		return newAWSProvider()
	case constants.CloudProviderAzure:
		return newAzureProvider()
	case constants.CloudProviderHetzner:
		return newHetznerProvider()
	case constants.CloudProviderBareMetal:
		return newBareMetalProvider()
	case constants.CloudProviderLocal:
		return newLocalProvider()
	default:
		panic("unknown provider: " + provider)
	}
}
