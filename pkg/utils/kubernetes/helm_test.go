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
	called  bool
	replace bool
	vals    map[string]any
	err     error
}

func (f *fakeInstallRunner) Run(_ *chart.Chart, vals map[string]any) (*release.Release, error) {
	f.called = true
	f.vals = vals
	if f.err != nil {
		return nil, f.err
	}
	return &release.Release{}, nil
}

type fakeHelmFactory struct {
	lister      *fakeListRunner
	installer   *fakeInstallRunner
	chartToLoad *chart.Chart
	chartErr    error
}

func (f *fakeHelmFactory) NewInstall(_, _ string, replace bool) HelmInstallRunner {
	f.installer.replace = replace
	return f.installer
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
		wantReplace     bool
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
			wantReplace:   false,
		},
		{
			name:          "no existing release — fresh install with Replace=false",
			releases:      nil,
			chartToLoad:   minimalChart(),
			wantInstalled: true,
			wantReplace:   false,
		},
		{
			name: "pending-install — recovers via Replace=true (no uninstall)",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusPendingInstall),
			},
			chartToLoad:   minimalChart(),
			wantInstalled: true,
			wantReplace:   true,
		},
		{
			name: "failed — recovers via Replace=true (no uninstall)",
			releases: []*release.Release{
				makeRelease(relName, ns, release.StatusFailed),
			},
			chartToLoad:   minimalChart(),
			wantInstalled: true,
			wantReplace:   true,
		},
		{
			name:          "non-nil Values — merges and passes to installer",
			releases:      nil,
			chartToLoad:   minimalChart(),
			values:        &values.Options{Values: []string{"foo=bar"}},
			wantInstalled: true,
			wantReplace:   false,
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
			assert.Equal(t, tc.wantReplace, installer.replace,
				"installer.Replace should reflect whether we're recovering a stuck release")
			if tc.wantValsKey != "" {
				require.NotNil(t, installer.vals)
				assert.Contains(t, installer.vals, tc.wantValsKey)
			}
		})
	}
}
