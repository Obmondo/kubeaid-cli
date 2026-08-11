// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

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
		// Where it actually wrote, which is not necessarily where it started:
		// starting fresh replaces the cluster name, and a replaced name moves
		// the files. Printing the directory we picked would send the operator
		// to the config they chose to leave alone.
		written, err := prompt.ConfigFromPrompt(globals.ConfigsDirectory, clusterName)
		if err != nil {
			assert.AssertErrNil(ctx, err, "Interactive config generation failed")
		}

		printNextStep(written.ConfigsDirectory, written.ClusterName)
	},
}

// Indirected so resolveTargetCluster is testable: huh needs a TTY.
var (
	confirmReuseDefaultConfigsDirectory = promptReuseDefaultConfigsDirectory
	askTargetCluster                    = promptTargetCluster
)

// resolveTargetCluster points globals.ConfigsDirectory at where this run's
// config should be written, and returns the cluster name when the per-cluster
// convention was used ("" when it was not).
//
// Precedence: an explicit --configs-directory wins, then --cluster-name, then
// an outputs/configs that already exists — but that last one is now offered
// rather than taken, because with no flags the default directory is always
// what gets looked at, and a config left in it names whichever cluster it was
// written for. Taken silently, that stale name is what the prompt pre-fills.
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

	// An existing directory with nothing in it is not something to reuse —
	// an aborted run leaves one behind — so there is nothing to ask about.
	if prompt.ExistingConfigPresent(globals.ConfigsDirectory) {
		reuse, err := confirmReuseDefaultConfigsDirectory(globals.ConfigsDirectory)
		if err != nil {
			return "", err
		}
		if reuse {
			return "", nil
		}
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

// promptReuseDefaultConfigsDirectory offers the config already sitting in the
// default directory, naming the cluster it was written for so that reusing it
// is a decision rather than something the operator discovers afterwards in a
// pre-filled form.
func promptReuseDefaultConfigsDirectory(configsDirectory string) (bool, error) {
	description := configsDirectory
	if name := clusterNameInConfigsDirectory(configsDirectory); name != "" {
		description += "\ncluster: " + name
	}

	reuse := true
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("A config already exists in the default directory. Use it?").
				Description(description).
				Affirmative("Use it").
				Negative("Choose a cluster").
				Value(&reuse),
		),
	).Run()
	if err != nil {
		return false, err
	}
	return reuse, nil
}

// clusterNameInConfigsDirectory peeks at the cluster name in a directory's
// general.yaml. Best-effort: the name only decorates a prompt the operator is
// already reading, so failing to read it must not replace that prompt with an
// error. A partial file from an interrupted run is the normal case here.
func clusterNameInConfigsDirectory(configsDirectory string) string {
	data, err := os.ReadFile(filepath.Join(configsDirectory, "general.yaml"))
	if err != nil {
		return ""
	}

	var general struct {
		Cluster struct {
			Name string `yaml:"name"`
		} `yaml:"cluster"`
	}
	if err := yaml.Unmarshal(data, &general); err != nil {
		return ""
	}
	return general.Cluster.Name
}

// newClusterOptionValue marks the "+ new cluster" entry in the picker.
//
// Deliberately not "": huh's Options() positions the cursor by comparing each
// option's value against whatever the accessor already holds, and an accessor
// with no value yet reads as the zero string. An empty sentinel therefore
// matched this entry, and Options() parks the scroll offset on the match — so
// the saved clusters started off-screen above it, and only appeared once a
// keypress recomputed the offset.
const newClusterOptionValue = "\x00new-cluster"

// promptTargetCluster asks which cluster this config is for.
//
// A picker is appropriate here in a way it is not for the cluster commands:
// `config generate` is already interactive, and choosing wrong pre-fills a
// form the operator then reviews rather than creating cloud infrastructure.
func promptTargetCluster() (string, error) {
	existing := clusterdir.List()
	if len(existing) == 0 {
		return askNewClusterName()
	}

	options := make([]huh.Option[string], 0, len(existing)+1)
	for _, name := range existing {
		options = append(options, huh.NewOption(name, name))
	}
	options = append(options, huh.NewOption("+ new cluster", newClusterOptionValue))

	choice := existing[0]
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which cluster is this config for?").
				Description("Configs live under ~/.config/kubeaid-cli/<cluster>/configs.").
				// Value before Options, which is what fixes the first frame:
				// Options() is where the cursor and the scroll offset are both
				// derived from the accessor, so the starting value has to be
				// in place by then. Set afterwards, it moved the cursor and
				// left the offset behind, and the list only straightened out
				// once a keypress recomputed it.
				Value(&choice).
				Options(options...),
		),
	).Run()
	if err != nil {
		return "", err
	}

	if choice == newClusterOptionValue {
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
