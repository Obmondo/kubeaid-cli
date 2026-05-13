// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

type bareMetalPrompter struct{}

func newBareMetalProvider() *bareMetalPrompter {
	return &bareMetalPrompter{}
}

func (p *bareMetalPrompter) SummaryLines(_ *PromptedConfig) []string {
	return nil
}

func (p *bareMetalPrompter) RunCredentialsForm(cfg *PromptedConfig, _ *autoDetectedConfig) error {
	cfg.BareMetalSSHPort = "22"
	cfg.BareMetalEndpointHost = cfg.ClusterName
	cfg.BareMetalEndpointPort = "6443"
	return nil
}
