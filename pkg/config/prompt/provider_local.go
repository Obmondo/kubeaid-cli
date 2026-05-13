// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

type localPrompter struct{}

func newLocalProvider() *localPrompter {
	return &localPrompter{}
}

func (p *localPrompter) SummaryLines(_ *PromptedConfig) []string {
	return nil
}

func (p *localPrompter) RunCredentialsForm(_ *PromptedConfig, _ *autoDetectedConfig) error {
	return nil
}
