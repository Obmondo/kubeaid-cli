package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	yqCmdLib "github.com/mikefarah/yq/v4/cmd"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/templates"
)

type SetupKubeAidConfigArgs struct {
	*CreateDevEnvArgs
	GitAuthMethod transport.AuthMethod
}

/*
Does the following :

	(1) Creates / updates all necessary files for the given cluster, in the user's KubeAid config
			repository.

	(2) Commits and pushes those changes to the upstream.

	(3) Waits for those changes to get merged into the default branch.

It expects the KubeAid Config repository to be already cloned in the temp directory.
*/
func SetupKubeAidConfig(ctx context.Context, args SetupKubeAidConfigArgs) {
	slog.InfoContext(ctx, "Setting up KubeAid config repo")

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
		newBranchName := fmt.Sprintf("kubeaid-%s-%d",
			config.ParsedGeneralConfig.Cluster.Name,
			time.Now().Unix(),
		)
		git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, args.GitAuthMethod)

		targetBranchName = newBranchName
	}

	clusterDir := utils.GetClusterDir()

	if !args.IsPartOfDisasterRecovery {
		// Create / update non Secret files.
		createOrUpdateNonSecretFiles(ctx, clusterDir, args.SkipMonitoringSetup)

		// Create / update Secret files.
		CreateOrUpdateSealedSecretFiles(ctx, clusterDir)
	} else {
		// Otherwise, if the main cluster is running on some cloud provider,
		// we just need to update the Kubernetes API server host and port in Cilium values file.
		mainClusterEndpoint := kubernetes.GetMainClusterEndpoint(ctx)
		if mainClusterEndpoint != nil {
			ciliumValuesFilePath := path.Join(clusterDir, "argocd-apps/values-cilium.yaml")

			yqCmd := yqCmdLib.New()
			yqCmd.SetArgs([]string{
				"--in-place", "--yaml-output", "--yaml-roundtrip",

				fmt.Sprintf(
					"(.cilium.k8sServiceHost) = \"%s\" | (.cilium.k8sServicePort) = \"%d\"",
					mainClusterEndpoint.Host, mainClusterEndpoint.Port,
				),

				ciliumValuesFilePath,
			})
			err := yqCmd.ExecuteContext(ctx)
			assert.AssertErrNil(ctx, err,
				"Failed updating main cluster's API server endpoint, in values-cilium.yaml file",
			)
		}
	}

	panic("checkpoint")

	// Add, commit and push the changes.
	commitMessage := fmt.Sprintf(
		"(cluster/%s) : created / updated KubeAid config files",
		config.ParsedGeneralConfig.Cluster.Name,
	)
	commitHash := git.AddCommitAndPushChanges(ctx,
		repo,
		workTree,
		targetBranchName,
		args.GitAuthMethod,
		config.ParsedGeneralConfig.Cluster.Name,
		commitMessage,
	)

	if !args.SkipPRWorkflow {
		/*
			The user now needs to go ahead and create a PR from the new to the default branch. Then he
			needs to merge that branch.

			NOTE : We can't create the PR for the user, since PRs are not part of the core git lib.
						 They are specific to the git platform the user is on.
		*/

		// Wait until the user creates a PR and merges it to the default branch.
		git.WaitUntilPRMerged(ctx,
			repo,
			defaultBranchName,
			commitHash,
			args.GitAuthMethod,
			targetBranchName,
		)
	}
}

// Creates / updates all non-secret files for the given cluster, in the user's KubeAid config
// repository.
func createOrUpdateNonSecretFiles(
	ctx context.Context,
	clusterDir string,
	skipMonitoringSetup bool,
) {
	// Get non Secret templates.
	embeddedTemplateNames := getEmbeddedNonSecretTemplateNames()
	templateValues := getTemplateValues(ctx)

	// Add KubePrometheus specific templates.
	// Then execute the Obmondo's KubePrometheus build script.
	if !skipMonitoringSetup {
		embeddedTemplateNames = append(embeddedTemplateNames,
			constants.TemplateNameKubePrometheusArgoCDApp,
		)

		buildKubePrometheus(ctx, clusterDir, templateValues)
	}

	// Create a file from each template.
	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(
			clusterDir,
			strings.TrimSuffix(embeddedTemplateName, ".tmpl"),
		)
		createFileFromTemplate(ctx, destinationFilePath, embeddedTemplateName, templateValues)
	}
}

// Creates / updates all necessary Sealed Secrets files for the given cluster, in the user's KubeAid
// config repository.
func CreateOrUpdateSealedSecretFiles(ctx context.Context, clusterDir string) {
	// Get Secret templates.
	embeddedTemplateNames := getEmbeddedSecretTemplateNames()
	templateValues := getTemplateValues(ctx)

	// Create a file from each template.
	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(
			clusterDir,
			strings.TrimSuffix(embeddedTemplateName, ".tmpl"),
		)
		createFileFromTemplate(ctx, destinationFilePath, embeddedTemplateName, templateValues)

		// Encrypt the Secret to a Sealed Secret.
		kubernetes.GenerateSealedSecret(ctx, destinationFilePath)
	}
}

// Creates file from the given template.
func createFileFromTemplate(ctx context.Context,
	destinationFilePath,
	embeddedTemplateName string,
	templateValues *TemplateValues,
) {
	utils.CreateIntermediateDirsForFile(ctx, destinationFilePath)

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", destinationFilePath),
	})

	// Open the destination file.
	destinationFile, err := os.OpenFile(
		destinationFilePath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0644,
	)
	assert.AssertErrNil(ctx, err, "Failed opening file")
	defer destinationFile.Close()

	// Execute the corresponding template with the template values. Then write the execution result
	// to that file.
	content := templates.ParseAndExecuteTemplate(ctx,
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
func buildKubePrometheus(ctx context.Context, clusterDir string, templateValues *TemplateValues) {
	// Create the jsonnet vars file.
	jsonnetVarsFilePath := fmt.Sprintf("%s/%s-vars.jsonnet",
		clusterDir,
		config.ParsedGeneralConfig.Cluster.Name,
	)
	createFileFromTemplate(ctx,
		jsonnetVarsFilePath,
		constants.TemplateNameKubePrometheusVars,
		templateValues,
	)

	// Create the kube-prometheus folder.
	kubePrometheusDir := fmt.Sprintf("%s/kube-prometheus", clusterDir)
	err := os.MkdirAll(kubePrometheusDir, os.ModePerm)
	assert.AssertErrNil(ctx, err,
		"Failed creating intermediate paths",
		slog.String("path", kubePrometheusDir),
	)

	// Run the KubePrometheus build script.
	slog.InfoContext(ctx, "Running KubePrometheus build script...")
	kubePrometheusBuildScriptPath := fmt.Sprintf("%s/build/kube-prometheus/build.sh",
		utils.GetKubeAidDir(),
	)
	utils.ExecuteCommandOrDie(fmt.Sprintf("%s %s", kubePrometheusBuildScriptPath, clusterDir))
}
