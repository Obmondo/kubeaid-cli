// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	yqCmdLib "github.com/mikefarah/yq/v4/cmd"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

type SetupKubeAidConfigArgs struct {
	*CreateDevEnvArgs
	GitAuthMethod transport.AuthMethod
}

/*
Does the following :

	(1) Creates / updates all necessary files for the given cluster, in the user's KubeAid config repository.

	(2) Commits and pushes those changes to the upstream.

	(3) Waits for those changes to get merged into the default branch.

Exception : when using the Bare Metal provider and the main cluster hasn't been provisioned yet,
only the KubeOne config file gets rendered - locally, skipping (2) and (3). KubeOne consumes that
local file, and it gets committed together with the rest of the cluster's KubeAid config files
after the main cluster has been provisioned - so the operator merges a single PR, instead of two.

It expects the KubeAid Config repository to be already cloned in the temp directory.
*/
func SetupKubeAidConfig(ctx context.Context, args SetupKubeAidConfigArgs) {
	slog.InfoContext(ctx, "Setting up KubeAid config repo")

	bar := progress.FromCtx(ctx)

	clusterDir := utils.GetClusterDir()

	// Determine whether this is part of setting up the main cluster.
	settingUpMainCluster := (os.Getenv(constants.EnvNameKubeconfig) == constants.OutputPathMainClusterKubeconfig)

	// We're using the Bare Metal provider, trying to provision the main cluster using KubeOne.
	// KubeOne consumes the locally rendered config file, so nothing needs to be pushed yet.
	if (globals.CloudProviderName == constants.CloudProviderBareMetal) &&
		!settingUpMainCluster && !args.IsPartOfDisasterRecovery {

		createOrUpdateKubeOneConfigFile(ctx, getTemplateValues(ctx), clusterDir)
		bar.Substep("Rendered KubeOne config (single kubeaid-config PR comes after provisioning)")

		return
	}

	repo, err := goGit.PlainOpen(utils.GetKubeAidConfigDir())
	assert.AssertErrNil(ctx, err, "Failed opening existing git repo")

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting worktree")

	defaultBranchName := git.GetDefaultBranchName(ctx, args.GitAuthMethod, repo)

	/*
		Decide the branch, where we want to do the changes :

		  (1) If the user has provided the --skip-pr-flow flag, then we'll do the changes in and push
		      them directly to the default branch.

		  (2) Otherwise, we'll create and checkout to a new branch. Changes will be done in and pushed
		      to that new branch. The user then needs to manually review the changes, create a PR and
		      merge it to the default branch.
	*/
	targetBranchName := defaultBranchName
	if !args.SkipPRWorkflow {
		// Create and checkout to a new branch.
		newBranchName := fmt.Sprintf(
			"kubeaid-%s-%d",
			config.ParsedGeneralConfig.Cluster.Name,
			time.Now().Unix(),
		)
		git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, args.GitAuthMethod)

		targetBranchName = newBranchName
	}

	// Determine whether the main cluster has already been provisioned.
	mainClusterEndpoint, endpointErr := kubernetes.GetMainClusterEndpoint(ctx)
	assert.AssertErrNil(ctx, endpointErr, "Failed getting main cluster endpoint")
	mainClusterProvisioned := (mainClusterEndpoint != nil)

	switch {
	// We're doing a disaster recovery. The only thing we need to consider is :
	// the main cluster's Kubernetes API server endpoint might have changed - for e.g., when you're
	// provisioning a VPN cluster in HCloud.
	// We need to reflect this change in the Cilium values file.
	case args.IsPartOfDisasterRecovery:
		if mainClusterProvisioned {
			ciliumValuesFilePath := path.Join(clusterDir, "argocd-apps/values-cilium.yaml")

			yqCmd := yqCmdLib.New()
			yqCmd.SetArgs([]string{
				"--in-place", "--yaml-output", "--yaml-roundtrip",

				fmt.Sprintf(
					"(.cilium.k8sServiceHost) = \"%s\" | (.cilium.k8sServicePort) = \"%s\"",
					mainClusterEndpoint.Hostname(), mainClusterEndpoint.Port(),
				),

				ciliumValuesFilePath,
			})
			err := yqCmd.ExecuteContext(ctx)
			assert.AssertErrNil(
				ctx, err,
				"Failed updating main cluster's API server endpoint, in values-cilium.yaml file",
			)
		}

	default:
		templateValues := getTemplateValues(ctx)

		// Create / update non Secret files.
		createOrUpdateNonSecretFiles(ctx, templateValues, clusterDir, args.SkipMonitoringSetup)

		// Create / update Secret files.
		createOrUpdateSealedSecretFiles(ctx, templateValues, clusterDir)
	}
	bar.Substep("Rendered kubeaid-config files")

	// Add, commit and push the changes.
	commitMessage := fmt.Sprintf(
		"(cluster/%s) : created / updated KubeAid config files",
		config.ParsedGeneralConfig.Cluster.Name,
	)
	commitHash := git.AddCommitAndPushChanges(
		ctx,
		repo,
		workTree,
		targetBranchName,
		args.GitAuthMethod,
		config.ParsedGeneralConfig.Cluster.Name,
		commitMessage,
		defaultBranchName,
	)

	// AddCommitAndPushChanges returns ZeroHash when the worktree was
	// already clean — kubeaid-config is up to date, nothing to push,
	// no noop PR to make the operator merge. Skip the rest of the
	// dance and surface a clear "already up to date" substep so the
	// flow still progresses visibly.
	if commitHash.IsZero() {
		bar.Substep("kubeaid-config already up to date")
		return
	}
	bar.Substep("Pushed kubeaid-config branch")

	if !args.SkipPRWorkflow {
		/*
			The user now needs to go ahead and create a PR from the new to the default branch. Then he
			needs to merge that branch.

			NOTE : We can't create the PR for the user, since PRs are not part of the core git lib.
						 They are specific to the git platform the user is on.
		*/

		// Wait until the user creates a PR and merges it to the default branch.
		git.WaitUntilPRMerged(
			ctx,
			repo,
			defaultBranchName,
			commitHash,
			args.GitAuthMethod,
			targetBranchName,
		)
		bar.Substep("Confirmed kubeaid-config PR merged")
	}
}

// Creates / updates all non-secret files for the given cluster, in the user's KubeAid config
// repository.
func createOrUpdateNonSecretFiles(ctx context.Context,
	templateValues *TemplateValues,
	clusterDir string,
	skipMonitoringSetup bool,
) {
	// Get non Secret templates.
	embeddedTemplateNames := getEmbeddedNonSecretTemplateNames()

	// Add the KubePrometheus ArgoCD App template name to the
	// render list — buildKubePrometheus depends on this file
	// existing on disk with the right kubeaid.io/version label
	// (build.sh inside the container reads it to verify the
	// pinned kubeaid version matches the local checkout). Order
	// matters: render the templates FIRST, then run build.sh.
	if !skipMonitoringSetup {
		embeddedTemplateNames = append(
			embeddedTemplateNames,
			constants.TemplateNameKubePrometheusArgoCDApp,
		)
	}

	// Create a file from each template.
	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(
			clusterDir,
			strings.TrimSuffix(embeddedTemplateName, ".tmpl"),
		)
		createFileFromTemplate(ctx, destinationFilePath, embeddedTemplateName, templateValues)
	}

	// Now that kube-prometheus.yaml is on disk with the current
	// KubeaidFork.Version label, run the build script. Reading a
	// stale-from-previous-bootstrap version label was the cause of
	// `Pinned kubeaid version 'X.Y.Z' not found locally` after a
	// version bump.
	if !skipMonitoringSetup {
		buildKubePrometheus(ctx, clusterDir, templateValues)
	}
}

// Creates / updates the KubeOne config file used to provision the main cluster, when using the
// Bare Metal provider.
func createOrUpdateKubeOneConfigFile(ctx context.Context, templateValues *TemplateValues, clusterDir string) {
	destinationFilePath := path.Join(
		clusterDir,
		strings.TrimSuffix(constants.KubeOneConfigTemlateName, ".tmpl"),
	)
	createFileFromTemplate(ctx, destinationFilePath, constants.KubeOneConfigTemlateName, templateValues)
}

// Creates / updates the cluster's kubeaid-cli.general.yaml copy in the KubeAid Config
// repository - the source of truth the KubeOne manifest derives from, kept next to it so a
// day-2 PR always carries both.
func createOrUpdateGeneralConfigFile(ctx context.Context, templateValues *TemplateValues, clusterDir string) {
	destinationFilePath := path.Join(
		clusterDir,
		strings.TrimSuffix(constants.TemplateNameGeneralConfig, ".tmpl"),
	)
	createFileFromTemplate(ctx, destinationFilePath, constants.TemplateNameGeneralConfig, templateValues)
}

// Creates / updates all necessary Sealed Secrets files for the given cluster, in the user's KubeAid
// config repository.
//
// Renders each Secret template to a buffer (not directly to disk) and hands the
// plaintext bytes to SealIfPlaintextChanged. That call short-circuits the
// kubeseal regeneration when the rendered plaintext matches the kubeaid-sha256
// header on the existing sealed file — kubeseal uses non-deterministic
// encryption (fresh AES key + nonce per Secret), so without the cache every
// re-run produces fresh ciphertext and a noisy PR full of identical-by-meaning
// sealed-secret diffs.
func createOrUpdateSealedSecretFiles(ctx context.Context, templateValues *TemplateValues, clusterDir string) {
	embeddedTemplateNames := getEmbeddedSecretTemplateNames()

	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(
			clusterDir,
			strings.TrimSuffix(embeddedTemplateName, ".tmpl"),
		)

		ctxWithPath := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("path", destinationFilePath),
		})

		err := utils.CreateIntermediateDirsForFile(destinationFilePath)
		assert.AssertErrNil(
			ctxWithPath, err,
			"Failed creating intermediate dirs",
		)

		plaintextBytes := templates.ParseAndExecuteTemplate(
			ctxWithPath,
			&KubeaidConfigFileTemplates,
			path.Join("templates/", embeddedTemplateName),
			templateValues,
		)

		if err := kubernetes.SealIfPlaintextChanged(ctxWithPath, destinationFilePath, plaintextBytes); err != nil {
			assert.AssertErrNil(ctxWithPath, err, "Failed generating sealed secret")
		}
	}
}

// Creates file from the given template.
func createFileFromTemplate(ctx context.Context,
	destinationFilePath,
	embeddedTemplateName string,
	templateValues *TemplateValues,
) {
	err := utils.CreateIntermediateDirsForFile(destinationFilePath)
	assert.AssertErrNil(
		ctx, err,
		"Failed creating intermediate dirs",
		slog.String("path", destinationFilePath),
	)

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", destinationFilePath),
	})

	// Open the destination file.
	destinationFile, err := os.OpenFile(
		destinationFilePath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	assert.AssertErrNil(ctx, err, "Failed opening file")
	defer destinationFile.Close()

	// Execute the corresponding template with the template values. Then write the execution result
	// to that file.
	content := templates.ParseAndExecuteTemplate(
		ctx,
		&KubeaidConfigFileTemplates,
		path.Join("templates/", embeddedTemplateName),
		templateValues,
	)
	_, err = destinationFile.Write(content)
	assert.AssertErrNil(ctx, err, "Failed writing template execution result to file")

	slog.InfoContext(ctx, "Created file in KubeAid config fork")
}

// Creates the jsonnet vars file for the cluster.
// Then executes KubeAid's kube-prometheus build script.
//
// build.sh needs the jsonnet toolchain (jsonnet, jb, gojsontoyaml,
// jq, util-linux for `column`). After the single-binary refactor
// kubeaid-cli no longer ships those, so the script runs inside a
// small docker image that does. The image is built on first use
// from the embedded Dockerfile (scripts/kube-prom-builder/Dockerfile);
// subsequent runs hit the docker layer cache and the build is a
// no-op.
//
// Mounts:
//
//	kubeAidDir (read-write) at the same host path inside the
//	  container — build.sh does `git -C "${basedir}/../.."` to
//	  read the kubeaid version, which needs the worktree
//	  visible.
//	clusterDir (read-write) at the same host path inside the
//	  container — script reads the *-vars.jsonnet and writes
//	  the generated manifests under <clusterDir>/kube-prometheus/.
//
// User: --user <hostUid>:<hostGid> so files written into clusterDir
// end up owned by the operator on the host, not by root.
func buildKubePrometheus(ctx context.Context, clusterDir string, templateValues *TemplateValues) {
	// Create the jsonnet vars file.
	jsonnetVarsFilePath := fmt.Sprintf(
		"%s/%s-vars.jsonnet",
		clusterDir,
		config.ParsedGeneralConfig.Cluster.Name,
	)
	createFileFromTemplate(
		ctx,
		jsonnetVarsFilePath,
		constants.TemplateNameKubePrometheusVars,
		templateValues,
	)

	// Create the kube-prometheus folder.
	kubePrometheusDir := fmt.Sprintf("%s/kube-prometheus", clusterDir)
	err := os.MkdirAll(kubePrometheusDir, 0o750)
	assert.AssertErrNil(
		ctx, err,
		"Failed creating intermediate paths",
		slog.String("path", kubePrometheusDir),
	)

	kubeAidDir := utils.GetKubeAidDir()

	// Build (or refresh) the kube-prom-builder image. Docker's
	// layer cache makes this a no-op once the image exists for the
	// current Dockerfile contents.
	ensureKubePromBuilderImage(ctx)

	// Run the KubePrometheus build script inside the builder image.
	//
	// kubeAidDir is bind-mounted at the same host absolute path so
	// build.sh's `git -C "${basedir}/../.."` resolves correctly
	// from inside the container without us having to translate
	// paths. clusterDir is bind-mounted at its host absolute path
	// for the same reason — the script's argv[1] is the host path
	// it reads / writes.
	//
	// --user pins the container process to the operator's
	// uid:gid so the generated kube-prometheus/ files end up
	// owned correctly on the host when the container exits.
	slog.InfoContext(ctx, "Running KubePrometheus build script in container...")
	hostUID := os.Getuid()
	hostGID := os.Getgid()
	err = runKubePrometheusBuilder(ctx, hostUID, hostGID, kubeAidDir, clusterDir, constants.KubePromBuilderImage)
	assert.AssertErrNil(ctx, err, "Failed running KubePrometheus build script")
}

func runKubePrometheusBuilder(
	ctx context.Context,
	hostUID, hostGID int,
	kubeAidDir string,
	clusterDir string,
	builderImage string,
) error {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("creating docker client: %w", err)
	}
	defer func() { _ = cli.Close() }()

	cmd := []string{
		fmt.Sprintf("%s/build/kube-prometheus/build.sh", kubeAidDir),
		clusterDir,
	}

	resp, err := cli.ContainerCreate(
		ctx,
		&container.Config{
			Image: builderImage,
			User:  fmt.Sprintf("%d:%d", hostUID, hostGID),
			Cmd:   cmd,
			Tty:   false,
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: kubeAidDir,
					Target: kubeAidDir,
				},
				{
					Type:   mount.TypeBind,
					Source: clusterDir,
					Target: clusterDir,
				},
			},
		},
		nil,
		nil,
		"",
	)
	if err != nil {
		return fmt.Errorf("creating builder container: %w", err)
	}

	// Better than AutoRemove=true because you can still read logs after exit.
	defer func() {
		_ = cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{
			Force: true,
		})
	}()

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting builder container: %w", err)
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)

	var statusCode int64
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("waiting for builder container: %w", err)
		}
	case status := <-statusCh:
		statusCode = status.StatusCode
	}

	logReader, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return fmt.Errorf("reading builder logs: %w", err)
	}
	defer logReader.Close()

	var stdout, stderr bytes.Buffer
	_, _ = stdcopy.StdCopy(&stdout, &stderr, logReader)

	if statusCode != 0 {
		return fmt.Errorf(
			"builder container failed with exit code %d\nstdout:\n%s\nstderr:\n%s",
			statusCode,
			stdout.String(),
			stderr.String(),
		)
	}

	return nil
}
