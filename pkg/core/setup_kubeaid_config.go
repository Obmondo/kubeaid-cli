package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/templates"
	goGit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

/*
Does the following :

	(1) Creates / updates all necessary files for the given cluster, in the user's KubeAid config
			repository.

	(2) Commits and pushes those changes to the upstream.

	(3) Waits for those changes to get merged into the default branch.

It expects the KubeAid Config repository to be already cloned in the temp directory.
*/
func SetupKubeAidConfig(ctx context.Context,
	gitAuthMethod transport.AuthMethod,
	skipKubePrometheusBuild bool,
) {
	slog.InfoContext(ctx, "Setting up KubeAid config repo")

	repo, err := goGit.PlainOpen(utils.GetKubeAidConfigDir())
	assert.AssertErrNil(ctx, err, "Failed opening existing git repo")

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting worktree")

	// Create and checkout to a new branch.
	newBranchName := fmt.Sprintf("kubeaid-%s-%d", config.ParsedConfig.Cluster.Name, time.Now().Unix())
	git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, gitAuthMethod)

	clusterDir := utils.GetClusterDir()

	// Create / update non Secret files.
	createOrUpdateKubeAidConfigFiles(ctx, clusterDir, gitAuthMethod, skipKubePrometheusBuild)

	// Create / update Secret files.
	CreateOrUpdateSealedSecretFiles(ctx, clusterDir)

	// Add, commit and push the changes.
	commitMessage := fmt.Sprintf(
		"(cluster/%s) : created / updated KubeAid config files",
		config.ParsedConfig.Cluster.Name,
	)
	commitHash := git.AddCommitAndPushChanges(ctx, repo, workTree, newBranchName, gitAuthMethod, config.ParsedConfig.Cluster.Name, commitMessage)

	// The user now needs to go ahead and create a PR from the new to the default branch. Then he
	// needs to merge that branch.
	// We can't create the PR for the user, since PRs are not part of the core git lib. They are
	// specific to the git platform the user is on.

	// Wait until the PR gets merged.
	defaultBranchName := git.GetDefaultBranchName(ctx, repo)
	git.WaitUntilPRMerged(ctx, repo, defaultBranchName, commitHash, gitAuthMethod, newBranchName)
}

// Creates / updates all necessary files for the given cluster, in the user's KubeAid config
// repository.
func createOrUpdateKubeAidConfigFiles(ctx context.Context,
	clusterDir string,
	gitAuthMethod transport.AuthMethod,
	skipKubePrometheusBuild bool,
) {
	// Get non Secret templates.
	embeddedTemplateNames := getEmbeddedNonSecretTemplateNames()
	templateValues := getTemplateValues()
	//
	// Create a file from each template.
	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(clusterDir, strings.TrimSuffix(embeddedTemplateName, ".tmpl"))
		createFileFromTemplate(ctx, destinationFilePath, embeddedTemplateName, templateValues)
	}

	// Build KubePrometheus.
	if !skipKubePrometheusBuild {
		buildKubePrometheus(ctx, clusterDir, gitAuthMethod, templateValues)
	}
}

// Creates / updates all necessary Sealed Secrets files for the given cluster, in the user's KubeAid
// config repository.
func CreateOrUpdateSealedSecretFiles(ctx context.Context, clusterDir string) {
	// Get Secret templates.
	embeddedTemplateNames := getEmbeddedSecretTemplateNames()
	templateValues := getTemplateValues()

	// Create a file from each template.
	for _, embeddedTemplateName := range embeddedTemplateNames {
		destinationFilePath := path.Join(clusterDir, strings.TrimSuffix(embeddedTemplateName, ".tmpl"))
		createFileFromTemplate(ctx, destinationFilePath, embeddedTemplateName, templateValues)

		// Encrypt the Secret to a Sealed Secret.
		utils.GenerateSealedSecret(ctx, destinationFilePath)
	}
}

// Creates file from the given template.
func createFileFromTemplate(ctx context.Context, destinationFilePath, embeddedTemplateName string, templateValues *TemplateValues) {
	utils.CreateIntermediateDirsForFile(ctx, destinationFilePath)

	// Open the destination file.
	destinationFile, err := os.OpenFile(destinationFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	assert.AssertErrNil(ctx, err, "Failed opening file", slog.String("path", destinationFilePath))
	defer destinationFile.Close()

	// Execute the corresponding template with the template values. Then write the execution result
	// to that file.
	content := templates.ParseAndExecuteTemplate(ctx, &KubeaidConfigFileTemplates, path.Join("templates/", embeddedTemplateName), templateValues)
	destinationFile.Write(content)

	slog.Info("Created file in KubeAid config fork", slog.String("file-path", destinationFilePath))
}

// Creates the jsonnet vars file for the cluster.
// Then executes KubeAid's kube-prometheus build script.
func buildKubePrometheus(ctx context.Context, clusterDir string, gitAuthMethod transport.AuthMethod, templateValues *TemplateValues) {
	// Create the jsonnet vars file.
	jsonnetVarsFilePath := fmt.Sprintf("%s/%s-vars.jsonnet", clusterDir, config.ParsedConfig.Cluster.Name)
	createFileFromTemplate(ctx, jsonnetVarsFilePath, constants.TemplateNameJsonnet, templateValues)

	// Create the kube-prometheus folder.
	kubePrometheusDir := fmt.Sprintf("%s/kube-prometheus", clusterDir)
	err := os.MkdirAll(kubePrometheusDir, os.ModePerm)
	assert.AssertErrNil(ctx, err, "Failed creating intermediate paths", slog.String("path", kubePrometheusDir))

	// If we're going to use the original KubeAid repo (https://github.com/Obmondo/KubeAid), then we
	// don't need any Git authentication method
	if config.ParsedConfig.Forks.KubeaidForkURL == constants.RepoURLObmondoKubeAid {
		gitAuthMethod = nil
	}

	// Clone the KubeAid fork locally (if not already cloned).
	kubeaidForkDir := constants.TempDir + "/kubeaid"
	git.CloneRepo(ctx, config.ParsedConfig.Forks.KubeaidForkURL, kubeaidForkDir, gitAuthMethod)

	// Run the KubePrometheus build script.
	slog.Info("Running KubePrometheus build script...")
	kubePrometheusBuildScriptPath := fmt.Sprintf("%s/build/kube-prometheus/build.sh", kubeaidForkDir)
	utils.ExecuteCommandOrDie(fmt.Sprintf("%s %s", kubePrometheusBuildScriptPath, clusterDir))
}
