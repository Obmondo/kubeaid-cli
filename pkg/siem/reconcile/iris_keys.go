// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package reconcile

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
)

// secretKeyStore keeps an IRIS service account's API key in one key of a
// Kubernetes Secret (iris.KeyStore).
type secretKeyStore struct {
	store secrets.Store
	ref   config.SecretRef
}

func (s secretKeyStore) Get(ctx context.Context) (string, error) {
	v, err := s.store.ReadRaw(ctx, s.ref)
	if err != nil {
		if apierrors.IsNotFound(err) || strings.Contains(err.Error(), "has no key") {
			return "", nil
		}
		return "", err
	}
	return string(v), nil
}

func (s secretKeyStore) Put(ctx context.Context, key string, dryRun bool) (report.Action, error) {
	a, _, err := secrets.Apply(ctx, s.store.Kube, s.ref.Namespace, s.ref.Name, map[string][]byte{s.ref.Key: []byte(key)}, dryRun)
	return a, err
}
