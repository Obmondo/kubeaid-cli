// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

type bareMetalPrompter struct{}

func newBareMetalProvider() *bareMetalPrompter {
	return &bareMetalPrompter{}
}

func (p *bareMetalPrompter) SummaryLines(cfg *PromptedConfig) []string {
	return []string{
		fmt.Sprintf("  Endpoint:      %s:%s", cfg.BareMetalEndpointHost, cfg.BareMetalEndpointPort),
		fmt.Sprintf("  Control plane: %d host(s)", len(cfg.BareMetalControlPlaneHosts)),
		fmt.Sprintf("  Workers:       %d host(s)", len(cfg.BareMetalWorkerHosts)),
	}
}

// RunCredentialsForm collects the generic bare-metal topology with the same
// flow as the Hetzner bare-metal prompt : add control-plane hosts one at a
// time, then workers, then the API endpoint — with a Continue/Back confirm
// between phases.
func (p *bareMetalPrompter) RunCredentialsForm(cfg *PromptedConfig, _ *autoDetectedConfig) error {
	if cfg.BareMetalSSHPort == "" {
		cfg.BareMetalSSHPort = "22"
	}
	if cfg.BareMetalEndpointPort == "" {
		cfg.BareMetalEndpointPort = "6443"
	}

	phase := genericBMPhaseCP
	for phase != genericBMPhaseDone {
		next, err := runGenericBareMetalPhase(cfg, phase)
		if err != nil {
			return err
		}
		phase = next
	}
	return nil
}

// defaultWorkerNodeGroupName is the node-group name both bare-metal prompt
// flows (generic and Hetzner) pre-fill — see promptWorkerNodeGroupName for
// why it's not cluster-name-prefixed.
const defaultWorkerNodeGroupName = "workers"

// genericBMPhase is one stage of the generic bare-metal flow — same state
// machine shape as the Hetzner bmPhase one, minus the Robot / vSwitch
// stages.
type genericBMPhase int

const (
	genericBMPhaseCP genericBMPhase = iota
	genericBMPhaseWorkers
	genericBMPhaseEndpoint
	genericBMPhaseDone
)

func runGenericBareMetalPhase(cfg *PromptedConfig, p genericBMPhase) (genericBMPhase, error) {
	switch p { //nolint:exhaustive // genericBMPhaseDone is the loop terminator; never passed in.
	case genericBMPhaseCP:
		cfg.BareMetalControlPlaneHosts = nil
		if err := addBareMetalHostLoop(cfg, roleControlPlane); err != nil {
			return genericBMPhaseDone, err
		}
		// Even counts lose etcd quorum — send the operator back around
		// instead of aborting the whole prompt.
		if len(cfg.BareMetalControlPlaneHosts)%2 == 0 {
			if err := noteEvenControlPlaneCount(len(cfg.BareMetalControlPlaneHosts)); err != nil {
				return genericBMPhaseDone, err
			}
			return genericBMPhaseCP, nil
		}
		goBack, err := promptPhaseTransition(
			fmt.Sprintf("Control plane configured — %d host(s) accepted", len(cfg.BareMetalControlPlaneHosts)),
			"Continue to workers",
			"← Re-enter control-plane hosts",
		)
		if err != nil {
			return genericBMPhaseDone, err
		}
		if goBack {
			return genericBMPhaseCP, nil
		}
		return genericBMPhaseWorkers, nil

	case genericBMPhaseWorkers:
		cfg.BareMetalWorkerHosts = nil
		if err := addBareMetalHostLoop(cfg, roleWorker); err != nil {
			return genericBMPhaseDone, err
		}
		if len(cfg.BareMetalWorkerHosts) > 0 {
			if err := promptBareMetalNodeGroupName(cfg); err != nil {
				return genericBMPhaseDone, err
			}
		}
		goBack, err := promptPhaseTransition(
			fmt.Sprintf("Workers configured — %d host(s) accepted", len(cfg.BareMetalWorkerHosts)),
			"Continue to endpoint",
			"← Back to control plane",
		)
		if err != nil {
			return genericBMPhaseDone, err
		}
		if goBack {
			return genericBMPhaseCP, nil
		}
		return genericBMPhaseEndpoint, nil

	case genericBMPhaseEndpoint:
		if err := promptGenericBareMetalEndpoint(cfg); err != nil {
			return genericBMPhaseDone, err
		}
		return genericBMPhaseDone, nil
	}
	return genericBMPhaseDone, fmt.Errorf("bare-metal: unreachable phase %d", p)
}

// addBareMetalHostLoop collects hosts for the given role, one form per host.
// Control-plane needs at least one host, so the first one is asked
// unconditionally and the "add another?" confirm follows each accepted host.
// Workers are optional — the confirm comes first, so answering "no" straight
// away yields a control-plane-only cluster.
func addBareMetalHostLoop(cfg *PromptedConfig, r role) error {
	for {
		if r == roleWorker {
			more, err := promptAddBareMetalHost(cfg, r)
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}

		host := ""
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					TitleFunc(func() string {
						return fmt.Sprintf("%s host #%d — address (DNS name or IP):",
							capitalize(r.label()), nextBareMetalHostIndex(cfg, r))
					}, cfg).
					Description("SSH reachable as root — KubeOne provisions Kubernetes over this address.").
					Value(&host).
					Validate(validateBareMetalHostAddress(cfg)),
			).Title(fmt.Sprintf("Bare Metal — %s host", r.label())),
		).Run(); err != nil {
			return err
		}

		host = strings.TrimSpace(host)
		switch r {
		case roleControlPlane:
			cfg.BareMetalControlPlaneHosts = append(cfg.BareMetalControlPlaneHosts, host)
		case roleWorker:
			cfg.BareMetalWorkerHosts = append(cfg.BareMetalWorkerHosts, host)
		}

		if r == roleControlPlane {
			more, err := promptAddBareMetalHost(cfg, r)
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	}
}

func nextBareMetalHostIndex(cfg *PromptedConfig, r role) int {
	if r == roleControlPlane {
		return len(cfg.BareMetalControlPlaneHosts) + 1
	}
	return len(cfg.BareMetalWorkerHosts) + 1
}

func promptAddBareMetalHost(cfg *PromptedConfig, r role) (bool, error) {
	count := len(cfg.BareMetalControlPlaneHosts)
	if r == roleWorker {
		count = len(cfg.BareMetalWorkerHosts)
	}

	title := fmt.Sprintf("Add another %s host?", r.label())
	negative := fmt.Sprintf("No, that's all (%d %s)", count, r.label())
	if (r == roleWorker) && (count == 0) {
		title = "Add a worker host?"
		negative = "No workers (control-plane only)"
	}

	var addAnother bool
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(fmt.Sprintf("So far: %d %s host(s).", count, r.label())).
				Affirmative(fmt.Sprintf("Yes, add %s #%d", r.label(), count+1)).
				Negative(negative).
				Value(&addAnother),
		),
	).Run()
	return addAnother, err
}

// validateBareMetalHostAddress rejects blanks and addresses already added in
// this flow — same inline-dup guard as the Hetzner server-ID field.
func validateBareMetalHostAddress(cfg *PromptedConfig) func(string) error {
	return func(s string) error {
		if err := nonEmpty(s); err != nil {
			return err
		}
		trimmed := strings.TrimSpace(s)
		for _, existing := range cfg.BareMetalControlPlaneHosts {
			if existing == trimmed {
				return fmt.Errorf("host %s is already added as a control-plane host", trimmed)
			}
		}
		for _, existing := range cfg.BareMetalWorkerHosts {
			if existing == trimmed {
				return fmt.Errorf("host %s is already added as a worker host", trimmed)
			}
		}
		return nil
	}
}

func noteEvenControlPlaneCount(count int) error {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Even control-plane count").
				Description(fmt.Sprintf(
					"%d control-plane hosts lose etcd quorum on a single failure — use an odd count (1, 3, 5). Re-enter the control-plane hosts.",
					count,
				)),
		),
	).Run()
}

func promptBareMetalNodeGroupName(cfg *PromptedConfig) error {
	if cfg.BareMetalNodeGroupName == "" {
		cfg.BareMetalNodeGroupName = defaultWorkerNodeGroupName
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Worker node-group name:").
				Description("Multi-group setups: edit general.yaml after generation to add more groups.").
				Value(&cfg.BareMetalNodeGroupName).
				Validate(nonEmpty),
		),
	).Run()
}

// promptGenericBareMetalEndpoint asks for the kube-apiserver endpoint,
// defaulting to the first control-plane host — NOT the cluster name, which
// usually doesn't resolve (this also repairs configs generated by older
// kubeaid-cli versions, which silently used the cluster name).
func promptGenericBareMetalEndpoint(cfg *PromptedConfig) error {
	if (cfg.BareMetalEndpointHost == "" || cfg.BareMetalEndpointHost == cfg.ClusterName) &&
		(len(cfg.BareMetalControlPlaneHosts) > 0) {
		cfg.BareMetalEndpointHost = cfg.BareMetalControlPlaneHosts[0]
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Control-plane endpoint (DNS name or IP):").
				Description("Must be reachable by every node on the endpoint port — defaults to the first control-plane host.").
				Value(&cfg.BareMetalEndpointHost).
				Validate(nonEmpty),
		).Title("Bare Metal — API endpoint"),
	).Run()
}
