// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"

	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// ensureHCloudCredentialsSecret kubectl-applies the kube-system/cloud-
// credentials Secret directly to the main cluster, breaking the
// bootstrap chicken-and-egg cycle:
//
//	hcloud-cloud-controller-manager needs cloud-credentials to start
//	→ until CCM starts, the node carries the
//	  node.cloudprovider.kubernetes.io/uninitialized:NoSchedule taint
//	→ which keeps sealed-secrets-controller Pending
//	→ which prevents the cloud-credentials SealedSecret from being
//	  decrypted into the matching Secret
//	→ so CCM never starts. Circular.
//
// We render the same SealedSecret into kubeaid-config for declarative
// state / DR-restore purposes — sealed-secrets-controller reconciles
// it idempotently once it's running, since the values match what we
// write here. Mirrors the pattern used for keycloak-admin on managed
// VPN clusters.
//
// No-op for non-Hetzner clusters and when the API token isn't set
// (the validator should have caught that earlier, but stay defensive).
func ensureHCloudCredentialsSecret(ctx context.Context, clusterClient client.Client) error {
	hetznerSecrets := config.ParsedSecretsConfig.Hetzner
	hetznerCfg := config.ParsedGeneralConfig.Cloud.Hetzner
	if hetznerSecrets == nil || hetznerCfg == nil {
		return nil
	}
	if hetznerSecrets.APIToken == "" {
		return fmt.Errorf(
			"secrets.yaml: hetzner.apiToken is empty — required for kube-system/cloud-credentials so the HCloud CCM can start",
		)
	}

	stringData := map[string]string{
		"hcloud": hetznerSecrets.APIToken,
	}

	// Bare-metal and hybrid modes also need robot credentials so the
	// CCM can manage the bare-metal side (matches the SealedSecret
	// template at sealed-secrets/kube-system/cloud-credentials.yaml.tmpl).
	if hetznerCfg.Mode == constants.HetznerModeBareMetal || hetznerCfg.Mode == constants.HetznerModeHybrid {
		if hetznerSecrets.Robot == nil || hetznerSecrets.Robot.User == "" || hetznerSecrets.Robot.Password == "" {
			return fmt.Errorf(
				"secrets.yaml: hetzner.robot.{user,password} required for cluster.cloud.hetzner.mode=%s — CCM needs them to manage bare-metal nodes",
				hetznerCfg.Mode,
			)
		}
		stringData["robot-user"] = hetznerSecrets.Robot.User
		stringData["robot-password"] = hetznerSecrets.Robot.Password
	}

	desired := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      constants.SecretNameCloudCredentials,
			Namespace: constants.NamespaceKubeSystem,
			Labels: map[string]string{
				"kubeaid.io/managed-by": "kubeaid",
			},
		},
		StringData: stringData,
	}

	existing := &coreV1.Secret{}
	getErr := clusterClient.Get(ctx,
		types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name},
		existing,
	)
	switch {
	case k8sAPIErrors.IsNotFound(getErr):
		if err := clusterClient.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil
	case getErr != nil:
		return fmt.Errorf("reading %s/%s: %w", desired.Namespace, desired.Name, getErr)
	}

	// Secret already there (re-run, or sealed-secrets-controller raced
	// us). Patch StringData so the token rotation case still updates.
	existing.StringData = desired.StringData
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels["kubeaid.io/managed-by"] = "kubeaid"
	if err := clusterClient.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating %s/%s: %w", desired.Namespace, desired.Name, err)
	}
	return nil
}
