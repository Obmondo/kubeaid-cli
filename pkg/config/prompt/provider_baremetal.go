// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

type bareMetalPrompter struct {
	baseProvider
}

func newBareMetalProvider() *bareMetalPrompter {
	p := &bareMetalPrompter{}
	p.questionsFunc = p.promptBareMetalQuestions
	return p
}

func (p *bareMetalPrompter) SummaryLines(_ *PromptedConfig) []string {
	return nil
}

func (p *bareMetalPrompter) promptBareMetalQuestions(cfg *PromptedConfig) error {
	cfg.BareMetalSSHPort = "22"
	cfg.BareMetalEndpointHost = cfg.ClusterName
	cfg.BareMetalEndpointPort = "6443"

	return nil
}
