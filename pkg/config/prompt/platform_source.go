// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"github.com/charmbracelet/huh"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

const (
	platformSourceOfficial = "official"
	platformSourceCustom   = "custom"
)

// runPlatformSourceFormFn is the test seam for the platform-source form.
var runPlatformSourceFormFn = runPlatformSourceForm

func platformSourceChoice(cfg *PromptedConfig) string {
	if cfg.KubeaidForkURL == constants.KubeAidPublicHTTPSURL {
		return platformSourceOfficial
	}
	return platformSourceCustom
}

func applyOfficialPlatformSource(cfg *PromptedConfig, detected *autoDetectedConfig) {
	cfg.KubeaidForkURL = constants.KubeAidPublicHTTPSURL
	cfg.KubeaidVersion = ""
	if detected != nil {
		cfg.KubeaidVersion = detected.KubeAidVersion
	}
}

func runPlatformSourceForm(cfg *PromptedConfig, detected *autoDetectedConfig) error {
	choice := platformSourceChoice(cfg)

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("KubeAid charts").
				Description("KubeAid CLI needs the Git repository containing the charts it installs.\n"+
					"Use KubeAid's maintained charts, or your own copy of those charts.\n"+
					"The official Obmondo/KubeAid repository is AGPL-3.0."),
			huh.NewSelect[string]().
				Title("Which charts do you want to use?").
				Options(
					huh.NewOption("Use KubeAid's maintained charts (AGPL-3.0)", platformSourceOfficial),
					huh.NewOption("Use my own copy of the KubeAid charts", platformSourceCustom),
				).
				Value(&choice),
		).Title("Chart source").Description("Step 4/5"),
	).Run(); err != nil {
		return err
	}

	if choice == platformSourceOfficial {
		applyOfficialPlatformSource(cfg, detected)
		return huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("KubeAid chart version (tag or branch):").
					Description("Auto-detected from the official KubeAid releases. Use a tag or branch, not a commit hash.").
					Value(&cfg.KubeaidVersion).
					Validate(platformSourceVersion),
			).Title("KubeAid charts").Description("Step 4/5"),
		).Run()
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Chart repository URL:").
				Description("HTTPS or SSH Git URL for your copy of the KubeAid charts.").
				Value(&cfg.KubeaidForkURL).
				Validate(gitRepositoryURL),
			huh.NewInput().
				Title("Chart version (tag or branch):").
				Description("Use a tag or branch, not a commit hash.").
				Value(&cfg.KubeaidVersion).
				Validate(platformSourceVersion),
		).Title("Your charts").Description("Step 4/5"),
	).Run()
}
