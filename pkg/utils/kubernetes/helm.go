package kubernetes

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/avast/retry-go/v4"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
)

type HelmInstallArgs struct {
	RepoURL,
	RepoName,
	ChartName,
	Version,
	ReleaseName,
	Namespace string
	Values map[string]interface{}
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

		slog.ErrorContext(ctx, "Failed searching for existing Helm release. Retrying after 10 seconds....", logger.Error(err))
		time.Sleep(10 * time.Second)
	}

	for _, release := range releases {
		if (release.Name == args.ReleaseName) && (release.Namespace == args.Namespace) {
			return release
		}
	}
	return nil
}

// Installs the given Helm chart.
func helmInstall(ctx context.Context,
	settings *cli.EnvSettings,
	actionConfig *action.Configuration,
	args *HelmInstallArgs,
) {
	slog.InfoContext(ctx, "Installing Helm chart")

	installAction := action.NewInstall(actionConfig)
	installAction.RepoURL = args.RepoURL
	installAction.Version = args.Version
	installAction.ReleaseName = args.ReleaseName
	installAction.Namespace = args.Namespace
	installAction.CreateNamespace = true
	installAction.Timeout = 10 * time.Minute
	installAction.Wait = true

	/*
		Determine the path to the Helm chart.
		We need to retry, since in some rare scenario we get this type of error :

		  Get "https://bitnami-labs.github.io/sealed-secrets/index.yaml": dial tcp: lookup
		  bitnami-labs.github.io on 127.0.0.11:53: server misbehaving
		  looks like "https://bitnami-labs.github.io/sealed-secrets/" is not a valid chart repository
		  or cannot be reached
	*/
	chartPath, _ := retry.DoWithData(func() (string, error) {
		return installAction.ChartPathOptions.LocateChart(args.ChartName, settings)
	})

	/*
		Load the Helm chart from that chart path.
		We need to retry, since sometimes on the first try, we get this error :

			looks like args.RepoURL is not a valid chart repository or cannot be reached.
			helm.sh/helm/v3/pkg/repo.FindChartInAuthAndTLSAndPassRepoURL
	*/
	chart, _ := retry.DoWithData(func() (*chart.Chart, error) {
		return loader.Load(chartPath)
	})

	// Install the Helm chart.
	_, err := installAction.Run(chart, args.Values)
	assert.AssertErrNil(ctx, err, "Failed installing Helm chart")
}
