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

	// Clone (or reuse) the KubeAid Config repository - the currently rendered KubeOne manifest
	// in there doubles as the fallback source of the cluster's current Kubernetes version.
	gitAuthMethod := git.GetGitAuthMethod(ctx)
	repo := git.CloneRepo(ctx, config.ParsedGeneralConfig.Forks.KubeaidConfigFork.URL, gitAuthMethod)

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

	if fromLiveCluster {
		assertAllNodesReady(ctx)
	}

	// CGroup v1 hosts cannot run Kubernetes versions beyond
	// constants.MaxCGroupV1CompatibleK8sVersion.
	if crossesCGroupV1Boundary(ctx, targetVersion) {
		verifyCGroupV2OnBareMetalHosts(ctx)
	}

	// (2) Re-render the KubeOne manifest from general.yaml and push it to the KubeAid Config
	//     repository.

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting kubeaid-config repo worktree")

	defaultBranchName := git.GetDefaultBranchName(ctx, gitAuthMethod, repo)

	targetBranchName := defaultBranchName
	if !args.SkipPRWorkflow {
		newBranchName := fmt.Sprintf(
			"kubeaid-%s-%d",
			config.ParsedGeneralConfig.Cluster.Name,
			time.Now().Unix(),
		)
		git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, gitAuthMethod)

		targetBranchName = newBranchName
	}

	createOrUpdateKubeOneConfigFile(ctx, getTemplateValues(ctx), utils.GetClusterDir())

	commitMessage := fmt.Sprintf(
		"(cluster/%s) : upgrading Kubernetes to %s",
		config.ParsedGeneralConfig.Cluster.Name,
		targetVersion,
	)
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

	// The cluster runs the target Kubernetes version already AND re-rendering changed nothing
	// (ZeroHash) - there's genuinely nothing to converge. When the manifest DID change (e.g.
	// kubelet tuning in general.yaml), proceed : 'kubeone apply' reconciles it onto the hosts.
	if alreadyAtTargetVersion && fromLiveCluster && commitHash.IsZero() {
		slog.InfoContext(
			ctx,
			"Cluster is already at the target Kubernetes version and the KubeOne manifest is unchanged, nothing to do",
			slog.String("version", targetVersion),
		)
		return
	}

	// ZeroHash means the rendered manifest didn't change (a previous run already pushed it).
	// There's nothing to merge then - skip straight to 'kubeone apply'.
	if !commitHash.IsZero() && !args.SkipPRWorkflow {
		git.WaitUntilPRMerged(
			ctx,
			repo,
			defaultBranchName,
			commitHash,
			gitAuthMethod,
			targetBranchName,
		)
	}

	// (3) Run 'kubeone apply' against the updated manifest.

	assertControlPlaneHostsNotHalfInitialized(ctx)
	assertBareMetalHostsPackageStateHealthy(ctx)
	runKubeOneApplyForUpgrade(ctx)

	// (4) Wait until every node reports the target kubelet version and is Ready.

	waitForNodesAtKubeletVersion(ctx, targetVersion)

	slog.InfoContext(
		ctx,
		"Cluster has been upgraded successfully 🎉🎉 !",
		slog.String("kubernetes-version", targetVersion),
	)
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
		return fmt.Errorf(
			"downgrade from %s to %s is not supported", currentVersion, targetVersion,
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

// runKubeOneApplyForUpgrade runs 'kubeone apply' (in-process) against the freshly rendered
// KubeOne manifest. Unlike during cluster provisioning, no --force-install : KubeOne detects
// the version diff and performs the rolling upgrade.
func runKubeOneApplyForUpgrade(ctx context.Context) {
	mainClusterName := config.ParsedGeneralConfig.Cluster.Name
	kubeoneDir := path.Join(utils.GetClusterDir(), "kubeone")

	slog.InfoContext(ctx, "Upgrading main cluster using Kubermatic KubeOne")

	err := runKubeOne(
		ctx, "upgrade",
		"apply",
		"--manifest", fmt.Sprintf("%s/kubeone-cluster.yaml", kubeoneDir),
		"--auto-approve",
	)
	assert.AssertErrNil(ctx, err, "Failed upgrading Kubernetes cluster using KubeOne")

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

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	for {
		if allNodesAtVersion(timeoutCtx, clusterClient, target) {
			return
		}

		select {
		case <-timeoutCtx.Done():
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
