// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/argoproj/argo-cd/v3/pkg/apiclient"
	"github.com/argoproj/argo-cd/v3/pkg/apiclient/application"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

// errNoArgoCDSession is returned by the ArgoCD operations of a Cluster that has not connected to
// ArgoCD yet.
var errNoArgoCDSession = errors.New("not connected to ArgoCD: install ArgoCD or call ConnectArgoCD first")

// Cluster runs the Kubernetes-side provisioning steps — Cluster API waits, Sealed Secrets and
// ArgoCD installation, ArgoCD syncs — for the cluster a config.Config describes. It owns the
// ArgoCD application client those steps share. Build one per cluster; it is not safe for
// concurrent use.
type Cluster struct {
	cfg *config.Config

	argoCD argoCDSession

	// reinstallSealedSecretsFn overrides ReinstallSealedSecrets in tests.
	reinstallSealedSecretsFn func(ctx context.Context) error
}

// argoCDSession is the port-forwarded ArgoCD application client of one cluster.
type argoCDSession struct {
	client application.ApplicationServiceClient
	closer io.Closer

	// clusterClient talks to the cluster ArgoCD runs in; it is kept to reconnect with.
	clusterClient client.Client
}

// NewCluster returns the Cluster for cfg, which must carry its General and Secrets configs.
func NewCluster(cfg *config.Config) (*Cluster, error) {
	if cfg == nil || cfg.General == nil || cfg.Secrets == nil {
		return nil, errors.New("a config with general and secrets configs is required")
	}

	return &Cluster{cfg: cfg}, nil
}

// Config returns the config the Cluster was built with.
func (c *Cluster) Config() *config.Config {
	return c.cfg
}

// ConnectArgoCD port-forwards the ArgoCD server through the ambient kubeconfig ($KUBECONFIG, else
// ~/.kube/config), reads the admin password through clusterClient, and opens the application
// client the ArgoCD operations use, replacing the previous one. A nil clusterClient reconnects
// through the one the previous connection was made with.
func (c *Cluster) ConnectArgoCD(ctx context.Context, clusterClient client.Client) error {
	if clusterClient == nil {
		clusterClient = c.argoCD.clusterClient
	}
	if clusterClient == nil {
		return errNoArgoCDSession
	}

	argoCDClient, err := NewArgoCDClient(ctx, clusterClient)
	if err != nil {
		return fmt.Errorf("failed creating ArgoCD client: %w", err)
	}

	return c.openArgoCDApplicationClient(ctx, argoCDClient, clusterClient)
}

// openArgoCDApplicationClient makes the application client of argoCDClient the Cluster's, and
// closes the one it replaces.
func (c *Cluster) openArgoCDApplicationClient(ctx context.Context,
	argoCDClient apiclient.Client,
	clusterClient client.Client,
) error {
	closer, applicationClient, err := argoCDClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed creating ArgoCD application client: %w", err)
	}

	if previous := c.argoCD.closer; previous != nil {
		if closeErr := previous.Close(); closeErr != nil {
			slog.DebugContext(ctx, "Closing the replaced ArgoCD application client failed",
				logger.Error(closeErr),
			)
		}
	}

	c.argoCD = argoCDSession{
		client:        applicationClient,
		closer:        closer,
		clusterClient: clusterClient,
	}
	return nil
}

// CloseArgoCD closes the ArgoCD application client, if there is one. A failure to close is
// logged: nothing can be done about it.
func (c *Cluster) CloseArgoCD(ctx context.Context) {
	closer := c.argoCD.closer
	c.argoCD = argoCDSession{}

	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		slog.WarnContext(ctx, "Closing the ArgoCD application client failed", logger.Error(err))
	}
}
