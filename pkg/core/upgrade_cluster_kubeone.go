// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"k8c.io/kubeone/pkg/executor"
	kubeonessh "k8c.io/kubeone/pkg/ssh"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

type UpgradeKubeOneClusterArgs struct {
	SkipPRWorkflow bool
}

var errK8sVersionAlreadyAtTarget = errors.New("cluster is already at the target Kubernetes version")

// UpgradeClusterUsingKubeOne upgrades a Bare Metal (KubeOne provisioned) cluster to the
// Kubernetes version declared in general.yaml (cluster.k8sVersion) : re-renders the KubeOne
// manifest in the kubeaid-config repo, pushes it (PR workflow unless skipped), then runs
// 'kubeone apply' in-process, which does the rolling upgrade (control-plane first, then the
// static workers). Idempotent - rerun to resume after a failure.
func UpgradeClusterUsingKubeOne(ctx context.Context, args UpgradeKubeOneClusterArgs) {
	targetVersion := config.ParsedGeneralConfig.Cluster.K8sVersion

	bar := progress.New("Upgrading cluster")
	defer bar.Finish()
	ctx = progress.WithBar(ctx, bar)
	bar.Describe(fmt.Sprintf("Upgrading cluster to Kubernetes %s", targetVersion))

	// Clone (or reuse) the KubeAid Config repository - the currently rendered KubeOne manifest
	// in there doubles as the fallback source of the cluster's current Kubernetes version.
	gitAuthMethod := git.GetGitAuthMethod(ctx)
	repo := git.CloneRepo(ctx, config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, gitAuthMethod)
	bar.Substep("Cloned kubeaid-config repo")

	// (1) Pre-flight.

	currentVersion, fromLiveCluster := getCurrentBareMetalClusterK8sVersion(ctx)
	slog.InfoContext(
		ctx, "Validated cluster Kubernetes version details",
		slog.String("current", currentVersion),
		slog.String("target", targetVersion),
		slog.Bool("read-from-live-cluster", fromLiveCluster),
	)

	err := validateK8sVersionHop(currentVersion, targetVersion)
	alreadyAtTargetVersion := errors.Is(err, errK8sVersionAlreadyAtTarget)
	if !alreadyAtTargetVersion {
		assert.AssertErrNil(ctx, err, "Kubernetes version upgrade rejected")
	}

	// Nothing to upgrade. Config-only changes (e.g. kubelet tuning) are 'cluster sync's job.
	// When the live cluster is unreachable, fall through instead : a previous upgrade run may
	// have died between the git push and 'kubeone apply', and rerunning the apply converges
	// the hosts.
	if alreadyAtTargetVersion && fromLiveCluster {
		slog.InfoContext(
			ctx,
			"Cluster is already at the target Kubernetes version, nothing to upgrade. Non-version changes in general.yaml get applied by 'kubeaid-cli cluster sync'",
			slog.String("version", targetVersion),
		)
		bar.Substep(fmt.Sprintf("Cluster already at Kubernetes %s - nothing to upgrade", targetVersion))
		bar.Substep("Non-version changes get applied by 'kubeaid-cli cluster sync'")
		return
	}
	bar.Describe(fmt.Sprintf("Upgrading Kubernetes : %s → %s", currentVersion, targetVersion))

	if fromLiveCluster {
		assertAllNodesReady(ctx)
		bar.Substep("All nodes Ready")
	}

	// CGroup v1 hosts cannot run Kubernetes versions beyond
	// constants.MaxCGroupV1CompatibleK8sVersion.
	if crossesCGroupV1Boundary(ctx, targetVersion) {
		verifyCGroupV2OnBareMetalHosts(ctx)
	}

	// (2) Re-render the KubeOne manifest from general.yaml and push it to the KubeAid Config
	//     repository.

	commitMessage := fmt.Sprintf(
		"(cluster/%s) : upgrading Kubernetes to %s",
		config.ParsedGeneralConfig.Cluster.Name,
		targetVersion,
	)
	pushKubeOneManifestChanges(ctx, repo, gitAuthMethod, commitMessage, args.SkipPRWorkflow)

	// (3) Run 'kubeone apply' against the updated manifest.

	assertControlPlaneHostsNotHalfInitialized(ctx)
	assertBareMetalHostsPackageStateHealthy(ctx)
	bar.Substep("Bare Metal host preflights passed")

	// On a single-node cluster, pod-selecting PodDisruptionBudgets deadlock KubeOne's drain
	// (nowhere for evicted pods to go). Remove them for the duration of the apply.
	removedPDBs, stopPDBGuard := neutralizeSingleNodePDBs(ctx)

	applyKubeOneManifest(ctx, "upgrade", false)
	stopPDBGuard()

	// (4) Wait until every node reports the target kubelet version and is Ready.

	waitForNodesAtKubeletVersion(ctx, targetVersion)

	restorePDBs(ctx, removedPDBs)

	slog.InfoContext(
		ctx,
		"Cluster has been upgraded successfully 🎉🎉 !",
		slog.String("kubernetes-version", targetVersion),
	)
	bar.Substep(fmt.Sprintf("Cluster upgraded to Kubernetes %s 🎉", targetVersion))
}

// validateK8sVersionHop enforces the KubeOne / kubeadm upgrade rules : the target version must
// be a patch bump within the current minor, or belong to the very next minor. Returns
// errK8sVersionAlreadyAtTarget when there's nothing to upgrade.
func validateK8sVersionHop(currentVersion, targetVersion string) error {
	current, err := version.ParseSemantic(currentVersion)
	if err != nil {
		return fmt.Errorf("parsing current Kubernetes version %q: %w", currentVersion, err)
	}

	target, err := version.ParseSemantic(targetVersion)
	if err != nil {
		return fmt.Errorf("parsing target Kubernetes version %q: %w", targetVersion, err)
	}

	switch {
	case target.EqualTo(current):
		return errK8sVersionAlreadyAtTarget

	case target.LessThan(current):
		// Spell out what IS valid from the running version, so the operator knows exactly
		// how to fix general.yaml.
		validTargets := fmt.Sprintf(
			"%s (the running version) or a newer v%d.%d patch",
			currentVersion, current.Major(), current.Minor(),
		)
		ceiling, ceilingErr := version.ParseGeneric(constants.MaxKubeOneSupportedK8sVersion)
		if (ceilingErr == nil) && (current.Minor() < ceiling.Minor()) {
			validTargets += fmt.Sprintf(
				", or the next minor v%d.%d.x", current.Major(), current.Minor()+1,
			)
		}

		return fmt.Errorf(
			"cluster.k8sVersion (%s) is behind the running cluster (%s), and Kubernetes can "+
				"never be downgraded. Set cluster.k8sVersion in general.yaml to %s - if you "+
				"truly need %s, re-installing the cluster with kubeaid-cli is much faster",
			targetVersion, currentVersion, validTargets, targetVersion,
		)

	case target.Major() != current.Major():
		return fmt.Errorf(
			"upgrading across major versions (%s to %s) is not supported",
			currentVersion, targetVersion,
		)

	case target.Minor() > (current.Minor() + 1):
		return fmt.Errorf(
			"cannot skip minor versions : the cluster is at %s, so first upgrade to v%d.%d, and then continue minor by minor towards %s",
			currentVersion, current.Major(), current.Minor()+1, targetVersion,
		)

	default:
		return nil
	}
}

// getCurrentBareMetalClusterK8sVersion determines the cluster's current Kubernetes version -
// preferring the live cluster (lowest kubelet version across nodes, so interrupted upgrades
// resume correctly), and falling back to the rendered KubeOne manifest in kubeaid-config.
func getCurrentBareMetalClusterK8sVersion(ctx context.Context) (string, bool) {
	if lowestKubeletVersion, found := getLowestNodeKubeletVersion(ctx); found {
		return lowestKubeletVersion, true
	}

	slog.WarnContext(
		ctx,
		"Couldn't reach the main cluster - falling back to the KubeOne manifest rendered in kubeaid-config, to determine the cluster's current Kubernetes version",
	)

	kubeOneManifestFilePath := path.Join(utils.GetClusterDir(), "kubeone/kubeone-cluster.yaml")
	kubeOneManifestContents, err := os.ReadFile(kubeOneManifestFilePath)
	assert.AssertErrNil(
		ctx, err,
		"Failed determining the cluster's current Kubernetes version : neither the main cluster is reachable, nor does a rendered KubeOne manifest exist in kubeaid-config",
	)

	parsedKubeOneManifest := struct {
		Versions struct {
			Kubernetes string `json:"kubernetes"`
		} `json:"versions"`
	}{}
	err = yaml.Unmarshal(kubeOneManifestContents, &parsedKubeOneManifest)
	assert.AssertErrNil(ctx, err, "Failed parsing rendered KubeOne manifest")

	assert.Assert(
		ctx, len(parsedKubeOneManifest.Versions.Kubernetes) > 0,
		"No Kubernetes version found in the rendered KubeOne manifest",
	)
	return parsedKubeOneManifest.Versions.Kubernetes, false
}

// getLowestNodeKubeletVersion returns the lowest kubelet version across the main cluster's
// nodes - using the lowest means interrupted upgrades resume correctly.
func getLowestNodeKubeletVersion(ctx context.Context) (string, bool) {
	clusterClient, err := getMainClusterClient(ctx)
	if err != nil {
		return "", false
	}

	nodes := &coreV1.NodeList{}
	if err := clusterClient.List(ctx, nodes); err != nil || len(nodes.Items) == 0 {
		slog.WarnContext(ctx, "Failed listing nodes of the main cluster")
		return "", false
	}

	var lowest *version.Version
	lowestRaw := ""
	for _, node := range nodes.Items {
		kubeletVersion, parseErr := version.ParseSemantic(node.Status.NodeInfo.KubeletVersion)
		if parseErr != nil {
			continue
		}
		if lowest == nil || kubeletVersion.LessThan(lowest) {
			lowest = kubeletVersion
			lowestRaw = node.Status.NodeInfo.KubeletVersion
		}
	}
	return lowestRaw, lowest != nil
}

// getMainClusterClient constructs a Kubernetes client for the main cluster, from the kubeconfig
// KubeOne generated during cluster provisioning.
func getMainClusterClient(ctx context.Context) (client.Client, error) {
	if _, err := os.Stat(constants.OutputPathMainClusterKubeconfig); err != nil {
		return nil, fmt.Errorf("main cluster kubeconfig not found: %w", err)
	}
	return kubernetes.CreateKubernetesClient(ctx, constants.OutputPathMainClusterKubeconfig)
}

// assertAllNodesReady fails the upgrade when any node is NotReady - 'kubeone apply' refuses
// unhealthy clusters anyway, so fail fast with a clear message instead.
func assertAllNodesReady(ctx context.Context) {
	clusterClient, err := getMainClusterClient(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing main cluster client")

	nodes := &coreV1.NodeList{}
	err = clusterClient.List(ctx, nodes)
	assert.AssertErrNil(ctx, err, "Failed listing nodes of the main cluster")

	notReadyNodeNames := []string{}
	for _, node := range nodes.Items {
		if !isNodeReady(&node) {
			notReadyNodeNames = append(notReadyNodeNames, node.Name)
		}
	}
	assert.Assert(
		ctx, len(notReadyNodeNames) == 0,
		fmt.Sprintf(
			"Cannot upgrade : node(s) %s are NotReady. Fix (or remove) them first",
			strings.Join(notReadyNodeNames, ", "),
		),
	)
}

func isNodeReady(node *coreV1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == coreV1.NodeReady {
			return condition.Status == coreV1.ConditionTrue
		}
	}
	return false
}

// crossesCGroupV1Boundary reports whether the target Kubernetes version is beyond the last
// CGroup v1 compatible one (constants.MaxCGroupV1CompatibleK8sVersion).
func crossesCGroupV1Boundary(ctx context.Context, targetVersion string) bool {
	target, err := version.ParseSemantic(targetVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing target Kubernetes version")

	maxCGroupV1Compatible, err := version.ParseMajorMinor(constants.MaxCGroupV1CompatibleK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing max CGroup v1 compatible Kubernetes version")

	return (target.Major() > maxCGroupV1Compatible.Major()) ||
		((target.Major() == maxCGroupV1Compatible.Major()) &&
			(target.Minor() > maxCGroupV1Compatible.Minor()))
}

// verifyCGroupV2OnBareMetalHosts SSHes into every Bare Metal host and verifies it runs CGroup
// v2 - kubelet versions beyond constants.MaxCGroupV1CompatibleK8sVersion refuse to start on
// CGroup v1 hosts, which would brick the node mid-upgrade.
func verifyCGroupV2OnBareMetalHosts(ctx context.Context) {
	slog.InfoContext(ctx, "Verifying that every Bare Metal host runs CGroup v2")

	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal
	connector := kubeonessh.NewConnector(ctx)

	hosts := []*config.BareMetalHost{}
	hosts = append(hosts, bareMetalConfig.ControlPlane.Hosts...)
	for _, nodeGroup := range bareMetalConfig.NodeGroups {
		hosts = append(hosts, nodeGroup.Hosts...)
	}

	for _, host := range hosts {
		connection := sshIntoBareMetalHost(ctx, host, connector)

		output, _, _, err := connection.Exec("stat -fc %T /sys/fs/cgroup")
		connection.Close()
		assert.AssertErrNil(ctx, err, "Failed detecting CGroup version on Bare Metal host")

		assert.Assert(
			ctx, strings.TrimSpace(output) == "cgroup2fs",
			fmt.Sprintf(
				"Bare Metal host %s still runs CGroup v1, which Kubernetes versions beyond %s don't support. Switch the host to CGroup v2 first (boot with systemd.unified_cgroup_hierarchy=1)",
				bareMetalHostAddress(host), constants.MaxCGroupV1CompatibleK8sVersion,
			),
		)
	}
}

// sshIntoBareMetalHost opens an SSH connection to the given Bare Metal host, trying its public
// and then its private address. The caller must close the returned connection.
func sshIntoBareMetalHost(
	ctx context.Context, host *config.BareMetalHost, connector *kubeonessh.Connector,
) executor.Interface {
	bareMetalConfig := config.ParsedGeneralConfig.Cloud.BareMetal

	sshAddresses := []string{}
	if host.PublicAddress != nil {
		sshAddresses = append(sshAddresses, *host.PublicAddress)
	}
	if host.PrivateAddress != nil {
		sshAddresses = append(sshAddresses, *host.PrivateAddress)
	}

	sshPort := bareMetalConfig.SSH.Port
	privateKey := ""
	switch {
	case (host.SSH != nil) && (host.SSH.SSHKeyPairConfig != nil):
		privateKey = host.SSH.PrivateKey
	case bareMetalConfig.SSH.SSHKeyPairConfig != nil:
		privateKey = bareMetalConfig.SSH.PrivateKey
	}
	if (host.SSH != nil) && (host.SSH.Port != 0) {
		sshPort = host.SSH.Port
	}

	var connection executor.Interface
	for _, address := range sshAddresses {
		ctxWithAddress := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("address", address),
		})

		opts := kubeonessh.Opts{
			Context:    ctxWithAddress,
			Hostname:   address,
			Port:       int(sshPort),
			Username:   "root",
			PrivateKey: []byte(privateKey),
			Timeout:    10 * time.Second,
		}
		if len(privateKey) == 0 {
			opts.AgentSocket = os.Getenv(constants.EnvNameSSHAuthSock)
		}

		var err error
		connection, err = kubeonessh.NewConnection(connector, opts)
		if err == nil {
			return connection
		}
		slog.WarnContext(
			ctxWithAddress, "SSH connection failed, trying next address",
			logger.Error(err),
		)
	}

	assert.Assert(
		ctx, false,
		fmt.Sprintf("Failed to SSH into Bare Metal host %s", bareMetalHostAddress(host)),
	)
	return nil
}

func bareMetalHostAddress(host *config.BareMetalHost) string {
	switch {
	case host.PublicAddress != nil:
		return *host.PublicAddress
	case host.PrivateAddress != nil:
		return *host.PrivateAddress
	default:
		return "<unknown>"
	}
}

// pushKubeOneManifestChanges re-renders the KubeOne manifest from general.yaml in the
// (already cloned) KubeAid Config repository, commits and pushes it, and - unless the PR
// workflow is skipped - blocks until the change is merged into the default branch. Returns
// whether the rendered manifest actually changed (false : a previous run already pushed this
// exact render, so there's nothing to merge).
func pushKubeOneManifestChanges(ctx context.Context,
	repo *goGit.Repository,
	gitAuthMethod transport.AuthMethod,
	commitMessage string,
	skipPRWorkflow bool,
) bool {
	bar := progress.FromCtx(ctx)

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting kubeaid-config repo worktree")

	defaultBranchName := git.GetDefaultBranchName(ctx, gitAuthMethod, repo)

	targetBranchName := defaultBranchName
	if !skipPRWorkflow {
		newBranchName := fmt.Sprintf(
			"kubeaid-%s-%d",
			config.ParsedGeneralConfig.Cluster.Name,
			time.Now().Unix(),
		)
		git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, gitAuthMethod)

		targetBranchName = newBranchName
	}

	// Render the derived KubeOne manifest AND the general.yaml copy it derives from, so the
	// pushed change carries the source of truth together with its effect.
	templateValues := getTemplateValues(ctx)
	createOrUpdateKubeOneConfigFile(ctx, templateValues, utils.GetClusterDir())
	createOrUpdateGeneralConfigFile(ctx, templateValues, utils.GetClusterDir())
	bar.Substep("Rendered kubeone-cluster.yaml and kubeaid-cli.general.yaml")

	commitHash := git.AddCommitAndPushChanges(
		ctx,
		repo,
		workTree,
		targetBranchName,
		gitAuthMethod,
		config.ParsedGeneralConfig.Cluster.Name,
		commitMessage,
		defaultBranchName,
	)
	if commitHash.IsZero() {
		bar.Substep("KubeOne manifest already up to date in kubeaid-config")

		return false
	}
	bar.Substep("Pushed kubeaid-config branch")

	if !skipPRWorkflow {
		releasePRWait := bar.InProgress("Waiting for you to merge the PR")
		git.WaitUntilPRMerged(
			ctx,
			repo,
			defaultBranchName,
			commitHash,
			gitAuthMethod,
			targetBranchName,
		)
		releasePRWait()
		bar.Substep("Confirmed PR merged")
	}

	return true
}

// applyKubeOneManifest runs 'kubeone apply' (in-process) against the freshly rendered KubeOne
// manifest. Unlike during cluster provisioning, no --force-install : KubeOne detects per-host
// version diffs itself and performs the rolling upgrade. On in-version hosts it runs its
// steady-state task set instead (helm releases, addons, joining new static workers,
// certificate renewal) - never a cordon / drain. forceUpgrade forces the upgrade tasks even
// on in-version hosts (rolling cordon + drain + kubelet restart) - the only way KubeOne
// rewrites kubelet flags without a version hop; callers gate it behind operator consent.
func applyKubeOneManifest(ctx context.Context, logLabel string, forceUpgrade bool) {
	mainClusterName := config.ParsedGeneralConfig.Cluster.Name
	kubeoneDir := path.Join(utils.GetClusterDir(), "kubeone")

	slog.InfoContext(
		ctx, "Applying KubeOne manifest onto the cluster hosts",
		slog.Bool("force-upgrade", forceUpgrade),
	)

	kubeoneArgs := []string{
		"apply",
		"--manifest", fmt.Sprintf("%s/kubeone-cluster.yaml", kubeoneDir),
		"--auto-approve",
	}
	if forceUpgrade {
		kubeoneArgs = append(kubeoneArgs, "--force-upgrade")
	}

	err := runKubeOne(ctx, logLabel, kubeoneArgs...)
	assert.AssertErrNil(ctx, err, "Failed applying KubeOne manifest onto the cluster hosts")

	// KubeOne backups the main cluster's PKI infrastructure in a .tar.gz file locally.
	// We don't need it.
	pkiBackupFilePath := fmt.Sprintf("%s/%s.tar.gz", kubeoneDir, mainClusterName)
	if err := os.Remove(pkiBackupFilePath); (err != nil) && !os.IsNotExist(err) {
		assert.AssertErrNil(
			ctx, err,
			"Failed deleting main cluster's PKI infrastructure backup",
		)
	}

	// KubeOne may re-save the main cluster's kubeconfig locally. Move it to our standardized
	// location, like during cluster provisioning.
	kubeoneGeneratedKubeconfigFilePath := fmt.Sprintf("%s-kubeconfig", mainClusterName)
	if _, err := os.Stat(kubeoneGeneratedKubeconfigFilePath); err == nil {
		err = utils.CreateIntermediateDirsForFile(constants.OutputPathMainClusterKubeconfig)
		assert.AssertErrNil(ctx, err, "Failed creating intermediate dirs for main cluster kubeconfig")

		err = utils.MoveFile(
			kubeoneGeneratedKubeconfigFilePath, constants.OutputPathMainClusterKubeconfig,
		)
		assert.AssertErrNil(ctx, err, "Failed moving KubeOne-generated kubeconfig")
	}

	progress.FromCtx(ctx).Substep("KubeOne apply completed")
}

// waitForNodesAtKubeletVersion polls the main cluster until every node is Ready and reports at
// least the target kubelet version. 'kubeone apply' already blocked until the upgrade
// finished, so this is a (bounded) confirmation step.
func waitForNodesAtKubeletVersion(ctx context.Context, targetVersion string) {
	target, err := version.ParseSemantic(targetVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing target Kubernetes version")

	clusterClient, err := getMainClusterClient(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing main cluster client")

	slog.InfoContext(ctx, "Waiting for all nodes to be Ready, at the target Kubernetes version")

	bar := progress.FromCtx(ctx)
	releaseWait := bar.InProgress("Waiting for every node to be Ready, at the target Kubernetes version")

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	for {
		if allNodesAtVersion(timeoutCtx, clusterClient, target) {
			releaseWait()
			bar.Substep("Every node Ready, at the target Kubernetes version")

			return
		}

		select {
		case <-timeoutCtx.Done():
			releaseWait()
			assert.Assert(
				ctx, false,
				"Timed out waiting for all nodes to be Ready at the target Kubernetes version. Rerun 'kubeaid-cli cluster upgrade' to resume",
			)
		case <-time.After(15 * time.Second):
		}
	}
}

func allNodesAtVersion(ctx context.Context, clusterClient client.Client, target *version.Version) bool {
	nodes := &coreV1.NodeList{}
	if err := clusterClient.List(ctx, nodes); err != nil || len(nodes.Items) == 0 {
		return false
	}

	for _, node := range nodes.Items {
		kubeletVersion, err := version.ParseSemantic(node.Status.NodeInfo.KubeletVersion)
		if err != nil || kubeletVersion.LessThan(target) || !isNodeReady(&node) {
			return false
		}
	}
	return true
}
