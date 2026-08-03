// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/config/prompt"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

// SampleConfigFileGeneral / SampleConfigFileSecrets stay embedded — they
// power tooling that still wants the hand-edit template shape (docs
// generation, integration tests that need a stable schema example).
// The `generate` command itself no longer writes them.
var (
	//go:embed templates/general.yaml
	SampleConfigFileGeneral string

	//go:embed templates/secrets.yaml
	SampleConfigFileSecrets string
)

var GenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Interactively generate general.yaml and secrets.yaml via the config prompt",
	Long: `Walks through an interactive prompt that collects all required values
(cluster basics, cloud-provider credentials, vSwitch / Hetzner Robot
hosts when applicable, Git / KubeAid fork URLs, etc.) and writes the
resulting general.yaml and secrets.yaml under --configs-directory.

cluster bootstrap consumes the files produced here; run config generate
first, review the output, then bootstrap.`,

	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		clusterName, err := resolveTargetCluster()
		assert.AssertErrNil(ctx, err, "Failed resolving which cluster to configure")

		// 0700, matching obmondo.Write: this directory holds secrets.yaml,
		// and the same location should not be group-traversable or not
		// depending on which command happened to create it.
		if err := os.MkdirAll(globals.ConfigsDirectory, 0o700); err != nil {
			assert.AssertErrNil(ctx, err,
				"Failed creating configs directory",
				slog.String("path", globals.ConfigsDirectory),
			)
		}

		// An existing config is not an error: the prompt offers to load it
		// as the pre-filled starting point (or start fresh), and only
		// rewrites the files after the final confirm.
		if err := prompt.ConfigFromPrompt(globals.ConfigsDirectory, clusterName); err != nil {
			assert.AssertErrNil(ctx, err, "Interactive config generation failed")
		}

		printNextStep(globals.ConfigsDirectory, clusterName)
	},
}

// resolveTargetCluster points globals.ConfigsDirectory at where this run's
// config should be written, and returns the cluster name when the per-cluster
// convention was used ("" when it was not).
//
// Precedence: an explicit --configs-directory wins, then --cluster-name, then
// an outputs/configs that already exists — so an operator who has been running
// kubeaid-cli from a working directory keeps the behaviour they have today.
// Only a genuinely fresh run asks.
func resolveTargetCluster() (string, error) {
	if !parser.UsingDefaultConfigsDirectory() {
		return "", nil
	}

	if globals.ClusterName != "" {
		directory, err := clusterdir.For(globals.ClusterName)
		if err != nil {
			return "", err
		}
		globals.ConfigsDirectory = directory
		return globals.ClusterName, nil
	}

	if _, err := os.Stat(globals.ConfigsDirectory); err == nil {
		return "", nil
	}

	name, err := askTargetCluster()
	if err != nil {
		return "", err
	}

	directory, err := clusterdir.For(name)
	if err != nil {
		return "", err
	}
	globals.ConfigsDirectory = directory

	return name, nil
}

// askTargetCluster asks which cluster this config is for.
//
// A picker is appropriate here in a way it is not for the cluster commands:
// `config generate` is already interactive, and choosing wrong pre-fills a
// form the operator then reviews rather than creating cloud infrastructure.
func askTargetCluster() (string, error) {
	const newCluster = ""

	existing := clusterdir.List()
	if len(existing) == 0 {
		return askNewClusterName()
	}

	options := make([]huh.Option[string], 0, len(existing)+1)
	for _, name := range existing {
		options = append(options, huh.NewOption(name, name))
	}
	options = append(options, huh.NewOption("+ new cluster", newCluster))

	choice := existing[0]
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which cluster is this config for?").
				Description("Configs live under ~/.config/kubeaid-cli/<cluster>/configs.").
				Options(options...).
				Value(&choice),
		),
	).Run()
	if err != nil {
		return "", err
	}

	if choice == newCluster {
		return askNewClusterName()
	}
	return choice, nil
}

func askNewClusterName() (string, error) {
	var name string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Cluster name").
				Description("Names the config directory, and is what --cluster-name takes later.").
				Value(&name).
				Validate(func(input string) error {
					if strings.TrimSpace(input) == "" {
						return errors.New("cluster name is required")
					}
					// Becomes a path segment, so a name containing a
					// separator would silently write somewhere else.
					if strings.ContainsAny(input, `/\`) {
						return errors.New("cluster name cannot contain a path separator")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(name), nil
}

// printNextStep spells out the command that consumes what was just written.
// Without the cluster name, `cluster bootstrap` would look in outputs/configs
// and not find it — so the operator needs the flag, and should not have to
// work that out from a not-found error.
func printNextStep(configsDirectory, clusterName string) {
	next := "kubeaid-cli cluster bootstrap"
	if clusterName != "" {
		next += " --" + constants.FlagNameClusterName + " " + clusterName
	}

	fmt.Fprintln(os.Stderr, lipgloss.NewStyle(). //nolint:forbidigo
							Border(lipgloss.RoundedBorder()).
							Padding(0, 1).
							Render(lipgloss.JoinVertical(
			lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Render("✓ Config written"),
			"",
			"  "+configsDirectory,
			"",
			lipgloss.NewStyle().Faint(true).Render("Next:"),
			"  "+next,
		)))
}
