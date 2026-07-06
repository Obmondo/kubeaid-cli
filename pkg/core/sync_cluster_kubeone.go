// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	kubeoneapi "k8c.io/kubeone/pkg/apis/kubeone"
	kubeonessh "k8c.io/kubeone/pkg/ssh"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type SyncKubeOneClusterArgs struct {
	SkipPRWorkflow bool
}

// SyncClusterUsingKubeOne reconciles a Bare Metal (KubeOne provisioned) cluster with
// general.yaml, without a Kubernetes version change (that's 'cluster upgrade's job) :
// re-renders the KubeOne manifest, pushes it (PR workflow unless skipped) and runs
// 'kubeone apply'. A plain apply reconciles the bundled helm releases and addons, joins newly
// added static workers, renews soon-to-expire certificates and re-labels nodes - without ever
// cordoning / draining in-version nodes.
//
// Kubelet tuning (cloud.bare-metal.kubelet) needs more : KubeOne rewrites the kubelet flags
// on a host only during its per-node upgrade procedure (cordon + drain + restart). When the
// hosts' kubelet flags differ from general.yaml, sync shows the drift and asks for consent -
// on approval the apply is forced (--force-upgrade), rolling the nodes one at a time; on
// decline (or without a TTY) the kubelet changes stay pending and the plain apply still runs.
func SyncClusterUsingKubeOne(ctx context.Context, args SyncKubeOneClusterArgs) {
	targetVersion := config.ParsedGeneralConfig.Cluster.K8sVersion

	bar := progress.New("Syncing cluster with general.yaml")
	defer bar.Finish()
	ctx = progress.WithBar(ctx, bar)
	bar.Describe("Syncing cluster with general.yaml")

	gitAuthMethod := git.GetGitAuthMethod(ctx)
	repo := git.CloneRepo(ctx, config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, gitAuthMethod)
	bar.Substep("Cloned kubeaid-config repo")

	// (1) Pre-flight.

	currentVersion, fromLiveCluster := getCurrentBareMetalClusterK8sVersion(ctx)

	err := validateK8sVersionHop(currentVersion, targetVersion)
	switch {
	case errors.Is(err, errK8sVersionAlreadyAtTarget):
		// The cluster and general.yaml agree on the version - exactly what sync expects.

	case err == nil:
		assert.Assert(ctx, false, fmt.Sprintf(
			"The cluster runs Kubernetes %s while general.yaml declares %s - run 'kubeaid-cli cluster upgrade' first. 'cluster sync' only reconciles non-version changes",
			currentVersion, targetVersion,
		))

	default:
		assert.AssertErrNil(ctx, err, "Rejecting cluster.k8sVersion in general.yaml")
	}
	bar.Substep(fmt.Sprintf("Cluster is at Kubernetes %s", currentVersion))

	if fromLiveCluster {
		assertAllNodesReady(ctx)
		bar.Substep("All nodes Ready")
	}

	// (2) Re-render the KubeOne manifest from general.yaml and push it to the KubeAid Config
	//     repository.

	commitMessage := fmt.Sprintf(
		"(cluster/%s) : syncing cluster config",
		config.ParsedGeneralConfig.Cluster.Name,
	)
	manifestChanged := pushKubeOneManifestChanges(
		ctx, repo, gitAuthMethod, commitMessage, args.SkipPRWorkflow,
	)
	if !manifestChanged {
		slog.InfoContext(
			ctx,
			"KubeOne manifest is unchanged - running 'kubeone apply' anyway : it reconciles helm releases / addons and renews soon-to-expire certificates, and a previous sync may have died before reaching it",
		)
	}

	// (3) Reconcile the hosts.

	bar.Describe("Reconciling cluster with KubeOne")

	assertControlPlaneHostsNotHalfInitialized(ctx)
	assertBareMetalHostsPackageStateHealthy(ctx)
	bar.Substep("Bare Metal host preflights passed")

	// Kubelet flags need KubeOne's per-node upgrade procedure - detect drift, then ask.
	forceUpgrade := false
	removedPDBs := []removedPDB{}
	stopPDBGuard := func() {}

	if driftedHosts := bareMetalKubeletFlagDrift(ctx); len(driftedHosts) > 0 {
		for _, driftedHost := range driftedHosts {
			bar.Substep(fmt.Sprintf(
				"Kubelet flags on host %s differ from general.yaml", driftedHost.hostAddress,
			))
		}

		if confirmKubeletReconcile(bar, driftedHosts) {
			forceUpgrade = true
			removedPDBs, stopPDBGuard = neutralizeSingleNodePDBs(ctx)
			bar.Substep("Reconciling kubelet flags : KubeOne rolls the nodes one at a time")
		} else {
			slog.InfoContext(
				ctx,
				"Kubelet reconcile declined - those changes stay pending until the next 'kubeaid-cli cluster upgrade', or a consented rerun of 'kubeaid-cli cluster sync'",
			)
			bar.Substep("Kubelet changes left pending (declined) - continuing with a plain apply")
		}
	}

	applyKubeOneManifest(ctx, "sync", forceUpgrade)
	stopPDBGuard()

	// (4) Wait until every node is Ready.

	waitForNodesAtKubeletVersion(ctx, targetVersion)

	restorePDBs(ctx, removedPDBs)

	slog.InfoContext(ctx, "Cluster is in sync with general.yaml 🎉")
	bar.Substep("Cluster in sync with general.yaml 🎉")
}

// kubeadmFlagsEnvFile is where KubeOne persists the per-host kubelet flags
// (kubeone pkg/tasks/common.go).
const kubeadmFlagsEnvFile = "/var/lib/kubelet/kubeadm-flags.env"

// kubeletFlagDrift describes one host whose kubelet flags differ from general.yaml.
type kubeletFlagDrift struct {
	hostAddress string
	// deltas holds one "--flag : current → desired" line per drifted flag.
	deltas []string
}

// bareMetalKubeletFlagDrift SSHes into every Bare Metal host and reports the ones whose
// kubeadm-flags.env doesn't carry the kubelet flags demanded by general.yaml. Empty when no
// kubelet tuning is configured. An unreadable flags file counts as drift - the forced apply
// rewrites it.
func bareMetalKubeletFlagDrift(ctx context.Context) []kubeletFlagDrift {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	desiredFlags := kubeletFlagsFromConfig(bareMetalConfig.Kubelet)
	if len(desiredFlags) == 0 {
		return nil
	}

	hosts := []*config.BareMetalHost{}
	hosts = append(hosts, bareMetalConfig.ControlPlane.Hosts...)
	for _, nodeGroup := range bareMetalConfig.NodeGroups {
		hosts = append(hosts, nodeGroup.Hosts...)
	}

	connector := kubeonessh.NewConnector(ctx)

	driftedHosts := []kubeletFlagDrift{}
	for _, host := range hosts {
		connection := sshIntoBareMetalHost(ctx, host, connector)
		stdout, _, _, err := connection.Exec("cat " + kubeadmFlagsEnvFile)
		connection.Close()

		deltas := []string{}
		switch {
		case err != nil:
			deltas = append(deltas, "kubeadm-flags.env is unreadable - counts as drift")
		default:
			deltas = kubeletFlagDeltas(desiredFlags, stdout)
		}

		if len(deltas) > 0 {
			driftedHosts = append(driftedHosts, kubeletFlagDrift{
				hostAddress: bareMetalHostAddress(host),
				deltas:      deltas,
			})
		}
	}

	return driftedHosts
}

// kubeletFlagsFromConfig maps cloud.bare-metal.kubelet from general.yaml onto the exact
// kubelet flags KubeOne writes into kubeadm-flags.env (kubeone pkg/tasks/kubeadm_env.go), so
// drift detection compares byte-for-byte what a converged host carries.
func kubeletFlagsFromConfig(kubeletConfig *config.BareMetalKubeletConfig) map[string]string {
	flags := map[string]string{}
	if kubeletConfig == nil {
		return flags
	}

	if m := kubeletConfig.SystemReserved; m != nil {
		flags["--system-reserved"] = kubeoneapi.MapStringStringToString(m, "=")
	}
	if m := kubeletConfig.KubeReserved; m != nil {
		flags["--kube-reserved"] = kubeoneapi.MapStringStringToString(m, "=")
	}
	if m := kubeletConfig.EvictionHard; m != nil {
		flags["--eviction-hard"] = kubeoneapi.MapStringStringToString(m, "<")
	}
	if m := kubeletConfig.MaxPods; m != nil {
		flags["--max-pods"] = strconv.Itoa(int(*m))
	}

	return flags
}

// kubeletFlagDeltas returns one "--flag : current → desired" line per desired flag that's
// missing from - or carries a different value in - the host's KUBELET_KUBEADM_ARGS line.
func kubeletFlagDeltas(desiredFlags map[string]string, kubeadmFlagsEnv string) []string {
	hostFlags := parseKubeadmFlagsEnv(kubeadmFlagsEnv)

	deltas := []string{}
	for flag, desiredValue := range desiredFlags {
		currentValue, present := hostFlags[flag]
		if !present {
			currentValue = "(unset)"
		}
		if currentValue != desiredValue {
			deltas = append(deltas, fmt.Sprintf("%s : %s → %s", flag, currentValue, desiredValue))
		}
	}
	sort.Strings(deltas)

	return deltas
}

// parseKubeadmFlagsEnv parses kubeadm-flags.env's KUBELET_KUBEADM_ARGS="--a=1 --b=2" line
// into a flag → value map (mirrors kubeone's own parser). Unparsable content yields an empty
// map, which the caller reads as drift.
func parseKubeadmFlagsEnv(content string) map[string]string {
	flags := map[string]string{}

	parts := strings.SplitN(strings.TrimSpace(content), "=", 2)
	if len(parts) != 2 {
		return flags
	}

	for _, flag := range strings.Split(strings.Trim(parts[1], `"`), " ") {
		if pair := strings.SplitN(flag, "=", 2); len(pair) == 2 {
			flags[pair[0]] = pair[1]
		}
	}

	return flags
}

// confirmKubeletReconcile asks the operator for consent to reconcile the drifted kubelet
// flags - KubeOne's per-node upgrade procedure cordons, drains and restarts one node at a
// time. Returns false when declined, and when the prompt itself can't run (no TTY) : the
// sync then proceeds with a plain, non-disruptive apply.
func confirmKubeletReconcile(bar *progress.Bar, driftedHosts []kubeletFlagDrift) bool {
	hostLines := make([]string, 0, len(driftedHosts))
	for _, driftedHost := range driftedHosts {
		hostLines = append(hostLines, fmt.Sprintf("  %s", driftedHost.hostAddress))
		for _, delta := range driftedHost.deltas {
			hostLines = append(hostLines, fmt.Sprintf("    %s", delta))
		}
	}

	description := fmt.Sprintf(
		"These hosts run kubelet flags that don't match general.yaml :\n\n%s\n\n"+
			"Applying them needs KubeOne's per-node upgrade procedure : each node gets\n"+
			"cordoned, drained and its kubelet restarted, one at a time.",
		strings.Join(hostLines, "\n"),
	)

	proceed := false

	bar.Pause()
	defer bar.Resume()

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Kubelet flags differ from general.yaml").
				Description(description),
			huh.NewConfirm().
				Title("Reconcile them now?").
				Affirmative("Yes, roll the nodes one by one").
				Negative("No, leave them for the next upgrade").
				Value(&proceed),
		),
	).Run(); err != nil {
		return false
	}

	return proceed
}
