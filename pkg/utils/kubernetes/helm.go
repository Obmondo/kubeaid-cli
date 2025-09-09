// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"log/slog"
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type HelmInstallArgs struct {
	ChartPath,

	ReleaseName,
	Namespace string

	Values *values.Options
}

// Installs the Helm chart (if not already deployed), present at the given local path.
// We clone the KubeAid repository locally and then use absolute path to one of it's Helm chart
// (like argo-cd / sealed-secrets), to install that corresponding Helm chart.
func HelmInstall(ctx context.Context, args *HelmInstallArgs) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("release-name", args.ReleaseName),
	})

	settings := cli.New()

	actionConfig := &action.Configuration{}
	err := actionConfig.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
		func(msg string, args ...any) {}, // Discard logs coming from the Helm Go SDK.
	)
	assert.AssertErrNil(ctx, err, "Failed initializing Helm action config")

	existingHelmRelease := findExistingHelmRelease(ctx, actionConfig, args)

	// CASE : Helm chart is already deployed. So we don't need to do anything.
	if (existingHelmRelease != nil) && (existingHelmRelease.Info.Status == release.StatusDeployed) {
		slog.InfoContext(ctx, "Skipped installing Helm chart, since it's already deployed")
		return
	}

	// CASE : Helm chart installation is stuck in pending-install state. So delete it first.
	//        Then we'll try to install it again.
	if (existingHelmRelease != nil) &&
		(existingHelmRelease.Info.Status == release.StatusPendingInstall) {
		slog.InfoContext(ctx, "Uninstalling Helm chart, stuck in pending-install state")

		uninstallAction := action.NewUninstall(actionConfig)
		uninstallAction.Timeout = 10 * time.Minute
		uninstallAction.Wait = true

		_, err = uninstallAction.Run(args.ReleaseName)
		assert.AssertErrNil(ctx, err, "Failed uninstalling Helm chart")
	}

	// Load and install the Helm chart.
	{
		// Load the custom values into map[string]any
		valuesMap := make(map[string]any)
		if args.Values != nil {
			p := getter.All(settings)
			valuesMap, err = args.Values.MergeValues(p)
			assert.AssertErrNil(ctx, err, "Failed merging the Helm chart values")
		}

		// Load Helm chart from the local chart path.
		chart, err := loader.Load(args.ChartPath)
		assert.AssertErrNil(ctx, err,
			"Failed loading Helm chart",
			slog.String("path", args.ChartPath),
		)

		// Install the Helm chart.
		slog.InfoContext(ctx, "Installing Helm chart....")

		installAction := action.NewInstall(actionConfig)
		installAction.ReleaseName = args.ReleaseName
		installAction.Namespace = args.Namespace
		installAction.CreateNamespace = true
		installAction.Timeout = 10 * time.Minute
		installAction.Wait = true

		_, err = installAction.Run(chart, valuesMap)
		assert.AssertErrNil(ctx, err, "Failed installing Helm chart")
	}
}

// Looks whether a Helm release with the given name exists or not.
// If yes, then returns it.
func findExistingHelmRelease(
	ctx context.Context,
	actionConfig *action.Configuration,
	args *HelmInstallArgs,
) *release.Release {
	listAction := action.NewList(actionConfig)
	listAction.AllNamespaces = true
	listAction.StateMask = action.ListAll
	listAction.Filter = args.ReleaseName

	// We need to retry, since sometimes the list operation may error out saying :
	//  tls: failed to verify certificate: x509: certificate signed by unknown authority
	var (
		releases []*release.Release
		err      error
	)
	for {
		releases, err = listAction.Run()
		if err == nil {
			break
		}

		slog.ErrorContext(ctx,
			"Failed searching for existing Helm release. Retrying after 10 seconds....",
			logger.Error(err),
		)
		time.Sleep(10 * time.Second)
	}

	for _, release := range releases {
		if (release.Name == args.ReleaseName) && (release.Namespace == args.Namespace) {
			return release
		}
	}
	return nil
}
