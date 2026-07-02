// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/release"
)

type fakeListResponse struct {
	releases []*release.Release
	err      error
}

// fakeListRunner cycles through queued responses. The last entry repeats once
// the queue is exhausted (same convention as fakeArgoCDAppClient.getResponses).
type fakeListRunner struct {
	responses []fakeListResponse
	called    int
}

func (f *fakeListRunner) Run() ([]*release.Release, error) {
	idx := f.called
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	f.called++
	r := f.responses[idx]
	return r.releases, r.err
}

type fakeInstallRunner struct {
	called bool
	vals   map[string]any
	err    error
}

func (f *fakeInstallRunner) Run(_ *chart.Chart, vals map[string]any) (*release.Release, error) {
	f.called = true
	f.vals = vals
	if f.err != nil {
		return nil, f.err
	}
	return &release.Release{}, nil
}

type fakeUpgradeRunner struct {
	called bool
	name   string
	vals   map[string]any
	err    error
}

func (f *fakeUpgradeRunner) Run(name string, _ *chart.Chart, vals map[string]any) (*release.Release, error) {
	f.called = true
	f.name = name
	f.vals = vals
	if f.err != nil {
		return nil, f.err
	}
	return &release.Release{}, nil
}

type fakeHelmFactory struct {
	lister      *fakeListRunner
	installer   *fakeInstallRunner
	upgrader    *fakeUpgradeRunner
	chartToLoad *chart.Chart
	chartErr    error
}

func (f *fakeHelmFactory) NewInstall(_, _ string) HelmInstallRunner { return f.installer }
func (f *fakeHelmFactory) NewUpgrade(_ string) HelmUpgradeRunner {
	if f.upgrader == nil {
		f.upgrader = &fakeUpgradeRunner{}
	}
	return f.upgrader
}
func (f *fakeHelmFactory) NewList(_ string) HelmListRunner { return f.lister }

func (f *fakeHelmFactory) LoadChart(_ string) (*chart.Chart, error) {
	return f.chartToLoad, f.chartErr
}

func singleResponseLister(releases []*release.Release) *fakeListRunner {
	return &fakeListRunner{responses: []fakeListResponse{{releases: releases}}}
}

func minimalChart() *chart.Chart {
	return &chart.Chart{Metadata: &chart.Metadata{Name: "test", Version: "0.1.0"}}
}

func makeRelease(name, ns string, status release.Status) *release.Release {
	return &release.Release{
		Name:      name,
		Namespace: ns,
		Info:      &release.Info{Status: status},
	}
}

func TestFindExistingHelmRelease(t *testing.T) {
	orig := helmListRetryDelay
	t.Cleanup(func() { helmListRetryDelay = orig })
	helmListRetryDelay = time.Millisecond

	const (
		relName = "my-release"
		ns      = "my-ns"
	)

	tests := []struct {
		name         string
		lister       *fakeListRunner
		args         *HelmInstallArgs
		wantNil      bool
		wantName     string
		wantListRuns int
	}{
		{
			name:    "returns nil when release list is empty",
			lister:  singleResponseLister(nil),
			args:    &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantNil: true,
		},
		{
			name: "returns nil when release name does not match",
			lister: singleResponseLister([]*release.Release{
				makeRelease("other-release", ns, release.StatusDeployed),
			}),
			args:    &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantNil: true,
		},
		{
			name: "returns nil when namespace does not match",
			lister: singleResponseLister([]*release.Release{
				makeRelease(relName, "other-ns", release.StatusDeployed),
			}),
			args:    &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantNil: true,
		},
		{
			name: "returns release when name and namespace both match",
			lister: singleResponseLister([]*release.Release{
				makeRelease(relName, ns, release.StatusDeployed),
			}),
			args:     &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantName: relName,
		},
		{
			name: "returns correct release when multiple releases are present",
			lister: singleResponseLister([]*release.Release{
				makeRelease("other-1", ns, release.StatusDeployed),
				makeRelease(relName, ns, release.StatusDeployed),
				makeRelease("other-2", ns, release.StatusDeployed),
			}),
			args:     &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantName: relName,
		},
		{
			name: "returns pending-install release when it matches",
			lister: singleResponseLister([]*release.Release{
				makeRelease(relName, ns, release.StatusPendingInstall),
			}),
			args:     &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantName: relName,
		},
		{
			name: "retries once after a transient error then succeeds",
			lister: &fakeListRunner{responses: []fakeListResponse{
				{err: errors.New("tls: certificate signed by unknown authority")},
				{releases: []*release.Release{makeRelease(relName, ns, release.StatusDeployed)}},
			}},
			args:         &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantName:     relName,
			wantListRuns: 2,
		},
		{
			name: "retries twice then succeeds",
			lister: &fakeListRunner{responses: []fakeListResponse{
				{err: errors.New("transient-1")},
				{err: errors.New("transient-2")},
				{releases: []*release.Release{makeRelease(relName, ns, release.StatusDeployed)}},
			}},
			args:         &HelmInstallArgs{ReleaseName: relName, Namespace: ns},
			wantName:     relName,
			wantListRuns: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := &fakeHelmFactory{
				lister:    tc.lister,
				installer: &fakeInstallRunner{},
			}

			got := findExistingHelmRelease(context.Background(), factory, tc.args)

			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantName, got.Name)
			if tc.wantListRuns > 0 {
				assert.Equal(t, tc.wantListRuns, tc.lister.called)
			}
		})
	}
}

func TestHelmInstallWithFactory(t *testing.T) {
	t.Parallel()

	const (
		relName = "argocd"
		ns      = "argocd"
	)

	tests := []struct {
		name            string
		releases        []*release.Release
		chartToLoad     *chart.Chart
		chartErr        error
		installErr      error
		values          *values.Options
		wantInstalled   bool
		wantValsKey     string
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "already deployed — skips install",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusDeployed),
			},
			wantInstalled: false,
		},
		{
			name:          "no existing release — fresh install",
			releases:      nil,
			chartToLoad:   minimalChart(),
			wantInstalled: true,
		},
		{
			name: "pending-install — install errors directing caller at upgrade",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusPendingInstall),
			},
			wantErr:         true,
			wantErrContains: "install-only",
		},
		{
			name: "failed — install errors directing caller at upgrade",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusFailed),
			},
			wantErr:         true,
			wantErrContains: "install-only",
		},
		{
			name:          "non-nil Values — merges and passes to installer",
			releases:      nil,
			chartToLoad:   minimalChart(),
			values:        &values.Options{Values: []string{"foo=bar"}},
			wantInstalled: true,
			wantValsKey:   "foo",
		},
		{
			name:            "LoadChart fails returns error",
			releases:        nil,
			chartErr:        errors.New("corrupt chart archive"),
			wantErr:         true,
			wantErrContains: "failed loading helm chart",
		},
		{
			name:            "install fails returns error",
			releases:        nil,
			chartToLoad:     minimalChart(),
			installErr:      errors.New("connection refused"),
			wantInstalled:   true,
			wantErr:         true,
			wantErrContains: "failed installing helm chart",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			installer := &fakeInstallRunner{err: tc.installErr}
			factory := &fakeHelmFactory{
				lister:      singleResponseLister(tc.releases),
				installer:   installer,
				chartToLoad: tc.chartToLoad,
				chartErr:    tc.chartErr,
			}

			err := helmInstallWithFactory(context.Background(), factory, &HelmInstallArgs{
				ChartPath:   t.TempDir(),
				ReleaseName: relName,
				Namespace:   ns,
				Values:      tc.values,
			})

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantInstalled, installer.called)
			if tc.wantValsKey != "" {
				require.NotNil(t, installer.vals)
				assert.Contains(t, installer.vals, tc.wantValsKey)
			}
		})
	}
}

func TestHelmUpgradeWithFactory(t *testing.T) {
	t.Parallel()

	const (
		relName = "sealed-secrets"
		ns      = "sealed-secrets"
	)

	tests := []struct {
		name            string
		releases        []*release.Release
		chartToLoad     *chart.Chart
		chartErr        error
		upgradeErr      error
		values          *values.Options
		wantUpgraded    bool
		wantValsKey     string
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "existing deployed release — upgrade succeeds",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusDeployed),
			},
			chartToLoad:  minimalChart(),
			wantUpgraded: true,
		},
		{
			name: "existing failed release — upgrade re-applies",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusFailed),
			},
			chartToLoad:  minimalChart(),
			wantUpgraded: true,
		},
		{
			name:            "no existing release — upgrade refuses, suggests install",
			releases:        nil,
			wantErr:         true,
			wantErrContains: "not found",
		},
		{
			name:         "non-nil Values — merges and passes to upgrader",
			releases:     []*release.Release{makeRelease(relName, ns, release.StatusDeployed)},
			chartToLoad:  minimalChart(),
			values:       &values.Options{Values: []string{"foo=bar"}},
			wantUpgraded: true,
			wantValsKey:  "foo",
		},
		{
			name:            "LoadChart fails returns error",
			releases:        []*release.Release{makeRelease(relName, ns, release.StatusDeployed)},
			chartErr:        errors.New("corrupt chart archive"),
			wantErr:         true,
			wantErrContains: "failed loading helm chart",
		},
		{
			name:            "upgrade fails returns error",
			releases:        []*release.Release{makeRelease(relName, ns, release.StatusDeployed)},
			chartToLoad:     minimalChart(),
			upgradeErr:      errors.New("connection refused"),
			wantUpgraded:    true,
			wantErr:         true,
			wantErrContains: "failed upgrading helm chart",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upgrader := &fakeUpgradeRunner{err: tc.upgradeErr}
			factory := &fakeHelmFactory{
				lister:      singleResponseLister(tc.releases),
				installer:   &fakeInstallRunner{}, // not used here, but interface needs it
				upgrader:    upgrader,
				chartToLoad: tc.chartToLoad,
				chartErr:    tc.chartErr,
			}

			err := helmUpgradeWithFactory(context.Background(), factory, &HelmInstallArgs{
				ChartPath:   t.TempDir(),
				ReleaseName: relName,
				Namespace:   ns,
				Values:      tc.values,
			})

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantUpgraded, upgrader.called)
			if tc.wantUpgraded {
				assert.Equal(t, relName, upgrader.name, "Run should pass the release name")
			}
			if tc.wantValsKey != "" {
				require.NotNil(t, upgrader.vals)
				assert.Contains(t, upgrader.vals, tc.wantValsKey)
			}
		})
	}
}

func TestHelmInstallOrUpgradeWithFactory(t *testing.T) {
	t.Parallel()

	const (
		relName = "argocd"
		ns      = "argocd"
	)

	tests := []struct {
		name            string
		releases        []*release.Release
		chartToLoad     *chart.Chart
		chartErr        error
		installErr      error
		upgradeErr      error
		values          *values.Options
		wantInstalled   bool
		wantUpgraded    bool
		wantValsKey     string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:          "no existing release -- fresh install, no upgrade",
			releases:      nil,
			chartToLoad:   minimalChart(),
			wantInstalled: true,
		},
		{
			name: "already deployed -- skips install, no upgrade",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusDeployed),
			},
			wantInstalled: false,
		},
		{
			// The bug this entry point fixes: a prior run left the
			// release "failed", so install-only refuses. Recover via
			// helm upgrade instead of surfacing the error.
			name: "failed release -- recovers via upgrade",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusFailed),
			},
			chartToLoad:  minimalChart(),
			wantUpgraded: true,
		},
		{
			name: "superseded release -- recovers via upgrade",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusSuperseded),
			},
			chartToLoad:  minimalChart(),
			wantUpgraded: true,
		},
		{
			// pending-* holds a release lock upgrade can't clear -- surface
			// the install-only error instead of attempting recovery.
			name: "pending-install -- not recovered, error propagates",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusPendingInstall),
			},
			wantErr:         true,
			wantErrContains: "install-only",
		},
		{
			// A non-recovery error from the install path (e.g. a
			// connection failure) must pass straight through, untouched.
			name:            "install fails with non-recovery error -- propagates, no upgrade",
			releases:        nil,
			chartToLoad:     minimalChart(),
			installErr:      errors.New("connection refused"),
			wantInstalled:   true,
			wantErr:         true,
			wantErrContains: "failed installing helm chart",
		},
		{
			name: "failed release with values -- upgrade receives merged values",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusFailed),
			},
			chartToLoad:  minimalChart(),
			values:       &values.Options{Values: []string{"foo=bar"}},
			wantUpgraded: true,
			wantValsKey:  "foo",
		},
		{
			// Recovery is attempted (install detected a failed release),
			// but the recovery upgrade itself fails (e.g. connection
			// drop mid-upgrade). The error from helmUpgradeWithFactory
			// must surface untouched -- not swallowed, not re-wrapped --
			// so callers see the real upgrade failure rather than a
			// misleading install-only error.
			name: "failed release -- upgrade recovery itself fails, error propagates",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusFailed),
			},
			chartToLoad:     minimalChart(),
			upgradeErr:      errors.New("connection refused"),
			wantUpgraded:    true,
			wantErr:         true,
			wantErrContains: "failed upgrading helm chart",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			installer := &fakeInstallRunner{err: tc.installErr}
			upgrader := &fakeUpgradeRunner{err: tc.upgradeErr}
			factory := &fakeHelmFactory{
				lister:      singleResponseLister(tc.releases),
				installer:   installer,
				upgrader:    upgrader,
				chartToLoad: tc.chartToLoad,
				chartErr:    tc.chartErr,
			}

			err := helmInstallOrUpgradeWithFactory(context.Background(), factory, &HelmInstallArgs{
				ChartPath:   t.TempDir(),
				ReleaseName: relName,
				Namespace:   ns,
				Values:      tc.values,
			})

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				assert.Equal(t, tc.wantUpgraded, upgrader.called, "upgrade attempt state should match expectation on the error path")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantInstalled, installer.called)
			assert.Equal(t, tc.wantUpgraded, upgrader.called)
			if tc.wantValsKey != "" {
				require.NotNil(t, upgrader.vals)
				assert.Contains(t, upgrader.vals, tc.wantValsKey)
			}
		})
	}
}
