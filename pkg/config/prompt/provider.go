// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// ProviderPrompter collects provider-specific (and provider-relevant common) config.
type ProviderPrompter interface {
	// PromptConfig asks all questions needed for this provider and populates cfg.
	PromptConfig(cfg *PromptedConfig, detected *autoDetectedConfig) error

	// SummaryLines returns provider-specific configuration details for the summary box.
	SummaryLines(cfg *PromptedConfig) []string
}

// baseProvider handles the common PromptConfig flow: provider questions → SSH auth.
// Embed this in provider structs and set questionsFunc to eliminate boilerplate.
type baseProvider struct {
	questionsFunc func(*PromptedConfig) error
}

// PromptConfig runs the standard flow: ask provider-specific questions, then handle SSH auth.
func (b *baseProvider) PromptConfig(cfg *PromptedConfig, detected *autoDetectedConfig) error {
	if b.questionsFunc != nil {
		if err := b.questionsFunc(cfg); err != nil {
			return fmt.Errorf("collecting provider config: %w", err)
		}
	}

	return promptSSHAuth(cfg, detected)
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
