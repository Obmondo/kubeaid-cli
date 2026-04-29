// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

var helmListRetryDelay = 10 * time.Second

type HelmInstallArgs struct {
	ChartPath,

	ReleaseName,
	Namespace string

	Values *values.Options
}

// HelmInstallRunner runs a single Helm install operation.
// Implementations must honour ReleaseName, Namespace, CreateNamespace, Timeout, and Wait
type HelmInstallRunner interface {
	Run(chrt *chart.Chart, vals map[string]any) (*release.Release, error)
}

// HelmUninstallRunner runs a single Helm uninstall operation.
type HelmUninstallRunner interface {
	Run(name string) (*release.UninstallReleaseResponse, error)
}

// HelmListRunner lists Helm releases.
type HelmListRunner interface {
	Run() ([]*release.Release, error)
}

// HelmActionFactory creates per-operation runners.
// Production wires this to *action.Configuration; tests provide a fake.
type HelmActionFactory interface {
	// NewInstall returns a runner configured for the given release name and namespace.
	NewInstall(releaseName, namespace string) HelmInstallRunner
	// NewUninstall returns a runner for uninstalling a named release.
	NewUninstall() HelmUninstallRunner
	// NewList returns a runner that lists releases matching the given filter.
	NewList(filter string) HelmListRunner
	// LoadChart loads a Helm chart from the given filesystem path.
	LoadChart(path string) (*chart.Chart, error)
}

// realHelmFactory adapts *action.Configuration to HelmActionFactory.
type realHelmFactory struct {
	cfg *action.Configuration
}

func (f *realHelmFactory) NewInstall(releaseName, namespace string) HelmInstallRunner {
	act := action.NewInstall(f.cfg)
	act.ReleaseName = releaseName
	act.Namespace = namespace
	act.CreateNamespace = true
	act.Timeout = 10 * time.Minute
	act.Wait = true
	return act
}

func (f *realHelmFactory) NewUninstall() HelmUninstallRunner {
	act := action.NewUninstall(f.cfg)
	act.Timeout = 10 * time.Minute
	act.Wait = true
	return act
}

func (f *realHelmFactory) NewList(filter string) HelmListRunner {
	act := action.NewList(f.cfg)
	act.AllNamespaces = true
	act.StateMask = action.ListAll
	act.Filter = filter
	return act
}

func (f *realHelmFactory) LoadChart(path string) (*chart.Chart, error) {
	return loader.Load(path)
}

// Installs the Helm chart (if not already deployed), present at the given local path.
// We clone the KubeAid repository locally and then use absolute path to one of it's Helm chart
// (like argo-cd / sealed-secrets), to install that corresponding Helm chart.
func HelmInstall(ctx context.Context, args *HelmInstallArgs) error {
	settings := cli.New()

	actionConfig := &action.Configuration{}
	err := actionConfig.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
		func(_ string, _ ...any) {}, // Discard logs coming from the Helm Go SDK.
	)
	if err != nil {
		return fmt.Errorf("failed initializing helm action config: %w", err)
	}

	return helmInstallWithFactory(ctx, &realHelmFactory{cfg: actionConfig}, args)
}

// helmInstallWithFactory is the unit-testable core of HelmInstall.
func helmInstallWithFactory(ctx context.Context, factory HelmActionFactory, args *HelmInstallArgs) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("release-name", args.ReleaseName),
	})

	existingHelmRelease := findExistingHelmRelease(ctx, factory, args)

	// CASE : Helm chart is already deployed. So we don't need to do anything.
	if (existingHelmRelease != nil) && (existingHelmRelease.Info.Status == release.StatusDeployed) {
		slog.InfoContext(ctx, "Skipped installing Helm chart, since it's already deployed")
		return nil
	}

	// CASE : Helm chart installation is stuck in pending-install state. So delete it first.
	//        Then we'll try to install it again.
	if (existingHelmRelease != nil) &&
		(existingHelmRelease.Info.Status == release.StatusPendingInstall) {
		slog.InfoContext(ctx, "Uninstalling Helm chart, stuck in pending-install state")

		uninstaller := factory.NewUninstall()
		if _, err := uninstaller.Run(args.ReleaseName); err != nil {
			return fmt.Errorf("failed uninstalling helm chart: %w", err)
		}
	}

	// Load and install the Helm chart.

	// Load the custom values into map[string]any
	valuesMap := make(map[string]any)
	if args.Values != nil {
		p := getter.All(cli.New())
		var err error
		valuesMap, err = args.Values.MergeValues(p)
		if err != nil {
			return fmt.Errorf("failed merging helm chart values: %w", err)
		}
	}

	// Load Helm chart from the local chart path.
	chrt, err := factory.LoadChart(args.ChartPath)
	if err != nil {
		return fmt.Errorf("failed loading helm chart %q: %w", args.ChartPath, err)
	}

	// Install the Helm chart.
	slog.InfoContext(ctx, "Installing Helm chart....")

	installer := factory.NewInstall(args.ReleaseName, args.Namespace)
	if _, err = installer.Run(chrt, valuesMap); err != nil {
		return fmt.Errorf("failed installing helm chart: %w", err)
	}

	return nil
}

// findExistingHelmRelease looks whether a Helm release with the given name exists.
// If yes, returns it.
func findExistingHelmRelease(ctx context.Context, factory HelmActionFactory, args *HelmInstallArgs) *release.Release {
	// We need to retry, since sometimes the list operation may error out saying :
	//  tls: failed to verify certificate: x509: certificate signed by unknown authority
	var (
		releases []*release.Release
		err      error
	)
	for {
		lister := factory.NewList(args.ReleaseName)
		releases, err = lister.Run()
		if err == nil {
			break
		}

		slog.ErrorContext(ctx,
			"Failed searching for existing Helm release. Retrying after 10 seconds....",
			logger.Error(err),
		)
		time.Sleep(helmListRetryDelay)
	}

	for _, rel := range releases {
		if (rel.Name == args.ReleaseName) && (rel.Namespace == args.Namespace) {
			return rel
		}
	}
	return nil
}
