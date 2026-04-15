// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

type localPrompter struct {
	baseProvider
}

func newLocalProvider() *localPrompter {
	return &localPrompter{} // questionsFunc is nil, so only promptSSHAuth runs
}

func (p *localPrompter) SummaryLines(_ *PromptedConfig) []string {
	return nil
}
