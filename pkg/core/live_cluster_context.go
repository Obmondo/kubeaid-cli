// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

// liveClusterContext identifies the cluster a day-2 command is about to act on.
//
// Nothing in kubeaid-cli ever selects a kubeconfig context : every client is built with
// clientcmd.BuildConfigFromFlags("", path), which silently follows whatever
// current-context the kubeconfig happens to carry. Commands that mutate a running
// cluster therefore have to show the operator which cluster that resolved to, rather
// than assume it is the one general.yaml describes.
type liveClusterContext struct {
	KubeconfigPath string
	ContextName    string
	ClusterName    string
	Server         string
}

// describeLiveClusterContext resolves the kubeconfig's current-context into the cluster
// it points at.
func describeLiveClusterContext(kubeconfigPath string) (*liveClusterContext, error) {
	apiConfig, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig %q : %w", kubeconfigPath, err)
	}

	if len(apiConfig.CurrentContext) == 0 {
		return nil, fmt.Errorf("kubeconfig %q has no current-context set", kubeconfigPath)
	}

	kubeContext, found := apiConfig.Contexts[apiConfig.CurrentContext]
	if !found {
		return nil, fmt.Errorf("kubeconfig %q sets current-context to %q, which it doesn't define",
			kubeconfigPath, apiConfig.CurrentContext,
		)
	}

	live := &liveClusterContext{
		KubeconfigPath: kubeconfigPath,
		ContextName:    apiConfig.CurrentContext,
		ClusterName:    kubeContext.Cluster,
	}

	if cluster, found := apiConfig.Clusters[kubeContext.Cluster]; found {
		live.Server = cluster.Server
	}

	return live, nil
}

// matchesConfiguredCluster reports whether the resolved context names the same cluster
// general.yaml does. A mismatch is not proof of a wrong target — a kubeconfig may name
// its cluster anything — but it is the signal that matters most when it is wrong.
func (l *liveClusterContext) matchesConfiguredCluster() bool {
	return l.ClusterName == config.ParsedGeneralConfig.Cluster.Name
}

// summary renders the context as an aligned block for the confirmation prompt.
func (l *liveClusterContext) summary(action string) string {
	lines := []string{
		fmt.Sprintf("  about to    : %s", action),
		fmt.Sprintf("  cluster     : %s   (from general.yaml)", config.ParsedGeneralConfig.Cluster.Name),
		fmt.Sprintf("  context     : %s", l.ContextName),
		fmt.Sprintf("  api server  : %s", l.Server),
		fmt.Sprintf("  kubeconfig  : %s", l.KubeconfigPath),
	}

	if !l.matchesConfiguredCluster() {
		lines = append(lines, "", fmt.Sprintf(
			"  MISMATCH : the kubeconfig's current-context points at cluster %q,\n"+
				"             but general.yaml describes %q. Check you are not about to\n"+
				"             reconcile one cluster's config onto another.",
			l.ClusterName, config.ParsedGeneralConfig.Cluster.Name,
		))
	}

	return strings.Join(lines, "\n")
}

// confirmLiveClusterContext makes the operator name the cluster before anything touches it.
//
// bar may be nil, for callers that run outside a progress section.
//
// Returns an error when the operator declines, when the kubeconfig can't be resolved, or
// when there is no terminal to ask on. Never proceeds by default : an unattended run of a
// command that rolls control-plane machines should stop, not guess.
func confirmLiveClusterContext(bar *progress.Bar,
	kubeconfigPath string,
	action string,
) error {
	live, err := describeLiveClusterContext(kubeconfigPath)
	if err != nil {
		return err
	}

	proceed := false

	if bar != nil {
		bar.Pause()
		defer bar.Resume()
	}

	title := "Confirm the target cluster"
	if !live.matchesConfiguredCluster() {
		title = "Confirm the target cluster (name mismatch)"
	}

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(title).
				Description(live.summary(action)),
			huh.NewConfirm().
				Title("Operate on this cluster?").
				Affirmative("Yes, this is the right cluster").
				Negative("No, abort").
				Value(&proceed),
		),
	).Run(); err != nil {
		return fmt.Errorf("confirming the target cluster %q : %w", live.ContextName, err)
	}

	if !proceed {
		return fmt.Errorf("aborted : declined to operate on cluster %q", live.ContextName)
	}

	return nil
}
