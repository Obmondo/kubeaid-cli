package utils

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
	"github.com/avast/retry-go/v4"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/strvals"
)

type HelmInstallArgs struct {
	RepoURL,
	RepoName,
	ChartName,
	Version,
	ReleaseName,
	Namespace string
	Values string
}

// Installs the given Helm chart (if not already deployed).
func HelmInstall(ctx context.Context, args *HelmInstallArgs) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{slog.String("chart", args.ChartName)})

	settings := cli.New()

	actionConfig := &action.Configuration{}
	err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), slog.Debug)
	assert.AssertErrNil(ctx, err, "Failed initializing Helm action config")

	existingHelmRelease := findExistingHelmRelease(ctx, actionConfig, args)

	// CASE : Helm chart is already deployed. So we don't need to do anything.
	if (existingHelmRelease != nil) && (existingHelmRelease.Info.Status == release.StatusDeployed) {
		slog.InfoContext(ctx, "Skipped installing Helm chart, since it's already deployed")
		return
	}

	// CASE : Helm chart installation is stuck in pending-install state. So delete it first. Then
	// we'll try to install it again.
	if (existingHelmRelease != nil) && (existingHelmRelease.Info.Status == release.StatusPendingInstall) {
		slog.InfoContext(ctx, "Uninstalling Helm chart, stuck in pending-install state")

		uninstallAction := action.NewUninstall(actionConfig)
		uninstallAction.Timeout = 10 * time.Minute
		uninstallAction.Wait = true

		_, err = uninstallAction.Run(args.ReleaseName)
		assert.AssertErrNil(ctx, err, "Failed uninstalling Helm chart")
	}

	// Install the Helm chart.
	helmInstall(ctx, settings, actionConfig, args)
}

// Looks whether a Helm release with the given name exists or not.
// If yes, then returns it.
func findExistingHelmRelease(ctx context.Context, actionConfig *action.Configuration, args *HelmInstallArgs) *release.Release {
	listAction := action.NewList(actionConfig)
	listAction.AllNamespaces = true
	listAction.StateMask = action.ListAll
	listAction.Filter = args.ReleaseName

	releases, err := listAction.Run()
	assert.AssertErrNil(ctx, err, "Failed searching for existing Helm release")

	for _, release := range releases {
		if (release.Name == args.ReleaseName) && (release.Namespace == args.Namespace) {
			return release
		}
	}
	return nil
}

// Installs the given Helm chart.
func helmInstall(ctx context.Context, settings *cli.EnvSettings, actionConfig *action.Configuration, args *HelmInstallArgs) {
	slog.InfoContext(ctx, "Installing Helm chart")

	installAction := action.NewInstall(actionConfig)
	installAction.RepoURL = args.RepoURL
	installAction.Version = args.Version
	installAction.ReleaseName = args.ReleaseName
	installAction.Namespace = args.Namespace
	installAction.CreateNamespace = true
	installAction.Timeout = 10 * time.Minute
	installAction.Wait = true

	// Determine the path to the Helm chart.
	chartPath, err := installAction.ChartPathOptions.LocateChart(args.ChartName, settings)
	assert.AssertErrNil(ctx, err, "Failed locating chart path in Helm repo")

	/*
		Load the Helm chart from that chart path.
		We need to retry, since sometimes on the first try, we get this error :

			looks like args.RepoURL is not a valid chart repository or cannot be reached.
			helm.sh/helm/v3/pkg/repo.FindChartInAuthAndTLSAndPassRepoURL
	*/
	chart, _ := retry.DoWithData(func() (*chart.Chart, error) {
		return loader.Load(chartPath)
	})

	// Parse Helm chart values.
	values, err := strvals.Parse(args.Values)
	assert.AssertErrNil(ctx, err, "Failed parsing Helm values")

	// Install the Helm chart.
	_, err = installAction.Run(chart, values)
	assert.AssertErrNil(ctx, err, "Failed installing Helm chart")
}
