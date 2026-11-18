// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/argoproj/argo-cd/v3/pkg/apiclient"
	"github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// testConfig returns an empty cluster config on the given cloud provider.
func testConfig(cloudProviderName string) *config.Config {
	return &config.Config{
		General:           &config.GeneralConfig{},
		Secrets:           &config.SecretsConfig{},
		CloudProviderName: cloudProviderName,
	}
}

// newTestCluster returns a Cluster over an empty config on the given cloud provider.
func newTestCluster(cloudProviderName string) *Cluster {
	return &Cluster{cfg: testConfig(cloudProviderName)}
}

type closeRecorder struct{ closed int }

func (c *closeRecorder) Close() error {
	c.closed++
	return nil
}

func TestNewCluster(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{name: "a nil config", cfg: nil, wantErr: true},
		{name: "a config without a general config", cfg: &config.Config{Secrets: &config.SecretsConfig{}}, wantErr: true},
		{name: "a config without a secrets config", cfg: &config.Config{General: &config.GeneralConfig{}}, wantErr: true},
		{name: "a complete config", cfg: testConfig(constants.CloudProviderHetzner)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster, err := NewCluster(tc.cfg)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, cluster)
				return
			}

			require.NoError(t, err)
			assert.Same(t, tc.cfg, cluster.Config())
		})
	}
}

func TestClusterArgoCDOperationsNeedAConnection(t *testing.T) {
	c := newTestCluster(constants.CloudProviderHetzner)
	ctx := context.Background()

	assert.ErrorIs(t, c.SyncAllArgoCDApps(ctx, true, nil), errNoArgoCDSession)
	assert.ErrorIs(t, c.SyncArgoCDApp(ctx, "root", nil), errNoArgoCDSession)
	assert.ErrorIs(t, c.WaitForArgoCDAppHealthy(ctx, "root"), errNoArgoCDSession)
	assert.ErrorIs(t, c.InstallAndSetupCrossplane(ctx), errNoArgoCDSession)
	assert.ErrorIs(t, c.ConnectArgoCD(ctx, nil), errNoArgoCDSession)
}

func TestClusterCloseArgoCD(t *testing.T) {
	t.Run("without a connection it does nothing", func(t *testing.T) {
		assert.NotPanics(t, func() { newTestCluster("").CloseArgoCD(context.Background()) })
	})

	t.Run("closes the connection once and forgets it", func(t *testing.T) {
		c := newTestCluster("")
		closer := &closeRecorder{}
		c.argoCD = argoCDSession{closer: closer, client: &fakeFullApplicationServiceClient{}}

		c.CloseArgoCD(context.Background())
		c.CloseArgoCD(context.Background())

		assert.Equal(t, 1, closer.closed)
		assert.Nil(t, c.argoCD.client)
	})
}

// fakeArgoCDAPIClient is the part of apiclient.Client that Cluster.openArgoCDApplicationClient
// uses.
type fakeArgoCDAPIClient struct {
	apiclient.Client

	closer io.Closer
	err    error
}

func (f *fakeArgoCDAPIClient) NewApplicationClient() (io.Closer, application.ApplicationServiceClient, error) {
	return f.closer, &fakeFullApplicationServiceClient{}, f.err
}

func TestClusterOpenArgoCDApplicationClient(t *testing.T) {
	ctx := context.Background()
	clusterClient := crFake.NewClientBuilder().Build()

	t.Run("a failure leaves the current connection alone", func(t *testing.T) {
		c := newTestCluster("")
		current := &closeRecorder{}
		c.argoCD = argoCDSession{closer: current, client: &fakeFullApplicationServiceClient{}}

		err := c.openArgoCDApplicationClient(ctx,
			&fakeArgoCDAPIClient{err: errors.New("port-forward died")}, clusterClient,
		)

		require.ErrorContains(t, err, "port-forward died")
		assert.Equal(t, 0, current.closed)
		assert.NotNil(t, c.argoCD.client)
	})

	t.Run("replaces and closes the previous connection, remembering the cluster client", func(t *testing.T) {
		c := newTestCluster("")
		previous := &closeRecorder{}
		c.argoCD = argoCDSession{closer: previous, client: &fakeFullApplicationServiceClient{}}
		fresh := &closeRecorder{}

		err := c.openArgoCDApplicationClient(ctx, &fakeArgoCDAPIClient{closer: fresh}, clusterClient)

		require.NoError(t, err)
		assert.Equal(t, 1, previous.closed)
		assert.Equal(t, 0, fresh.closed)
		assert.Same(t, fresh, c.argoCD.closer)
		assert.NotNil(t, c.argoCD.client)
		assert.Equal(t, clusterClient, c.argoCD.clusterClient)
	})
}
