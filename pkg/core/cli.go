// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
	templateUtils "github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// cliConfig returns the explicit config of the cluster the CLI parsed, which pkg/core hands to the
// library packages. The config parser builds it; it is built again from the parsed globals when
// those have moved on since (tests swap them).
func cliConfig() *config.Config {
	cfg := globals.Config
	if cfg != nil &&
		cfg.General == config.ParsedGeneralConfig &&
		cfg.Secrets == config.ParsedSecretsConfig &&
		cfg.CloudProviderName == globals.CloudProviderName {
		return cfg
	}

	globals.Config = &config.Config{
		General:           config.ParsedGeneralConfig,
		Secrets:           config.ParsedSecretsConfig,
		CloudProviderName: globals.CloudProviderName,
	}
	globals.Cluster = nil
	return globals.Config
}

// kubeCluster returns the Kubernetes-side operations of the cluster the CLI parsed. It is one
// instance for the whole run, since it owns the ArgoCD connection.
func kubeCluster() *kubernetes.Cluster {
	cfg := cliConfig()
	if globals.Cluster == nil || globals.Cluster.Config() != cfg {
		cluster, err := kubernetes.NewCluster(cfg)
		assert.AssertErrNil(context.Background(), err, "Failed building the cluster operations")
		globals.Cluster = cluster
	}
	return globals.Cluster
}

// gitClient returns the git client of the cluster the CLI parsed.
func gitClient() *git.Client {
	return git.New(cliConfig())
}

// assertGit exits the process on a failed git operation, with the log line the operation printed
// back when pkg/utils/git asserted by itself: the headline of the git.Error, its attributes, and
// its cause as the error attribute.
func assertGit(ctx context.Context, err error) {
	if err == nil {
		return
	}

	message, attrs, cause := gitFailure(err)
	assert.AssertErrNil(ctx, cause, message, attrs...)
}

// gitFailure returns what assertGit exits with for a non-nil err.
func gitFailure(err error) (message string, attrs []any, cause error) {
	var gitErr *git.Error
	if !errors.As(err, &gitErr) {
		return "Git operation failed", nil, err
	}

	for _, attr := range gitErr.Attrs {
		attrs = append(attrs, attr)
	}

	if gitErr.Err == nil {
		return gitErr.Message, attrs, err
	}
	return gitErr.Message, attrs, gitErr.Err
}

// isClusterctlMoveExecuted returns whether the 'clusterctl move' command has already been
// executed: whether the main cluster is reachable and holds the Cluster resource.
func isClusterctlMoveExecuted(ctx context.Context) bool {
	mainClusterClient, err := kubernetes.CreateKubernetesClient(ctx,
		constants.OutputPathMainClusterKubeconfig,
	)
	// Main cluster isn't reachable,
	// which means 'clusterctl move' hasn't been executed.
	if err != nil {
		return false
	}

	return kubeCluster().IsClusterctlMoveExecuted(ctx, mainClusterClient)
}

// assertTemplate exits the process on a failed template rendering, with the message the rendering
// printed back when pkg/utils/templates asserted by itself.
func assertTemplate(ctx context.Context, err error) {
	if err == nil {
		return
	}

	message, attrs, cause := templateFailure(err)
	assert.AssertErrNil(ctx, cause, message, attrs...)
}

// templateFailure returns what assertTemplate exits with for a non-nil err.
func templateFailure(err error) (message string, attrs []any, cause error) {
	var templateErr *templateUtils.Error
	if !errors.As(err, &templateErr) {
		return "Failed rendering template", nil, err
	}

	message = "Failed executing template"
	switch templateErr.Stage {
	case templateUtils.StageReading:
		message = "Failed getting template from embedded file-system"
	case templateUtils.StageParsing:
		message = "Failed parsing template"
	case templateUtils.StageExecuting:
	}

	return message, []any{slog.String("template", templateErr.Template)}, templateErr.Err
}
