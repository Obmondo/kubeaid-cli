// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"fmt"
	"net/url"
	"time"

	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// netBirdPostgresAppSecret is CloudNativePG's auto-rendered
// application credentials Secret for the NetBird postgres Cluster
// (kubeaid-addons creates the Cluster as `<instanceName>-pgsql`,
// CNPG appends `-app` for the app-level role). NetBird Mgmt's
// Cluster is named `netbird-pgsql`, so its app secret is
// `netbird-pgsql-app`.
const (
	netBirdPostgresAppSecret = "netbird-pgsql-app"

	// CNPG read-write Service: NetBird Mgmt always wants writes,
	// so we point its DSN at -rw rather than -ro.
	netBirdPostgresHost = "netbird-pgsql-rw.netbird"
	netBirdPostgresPort = "5432"
	netBirdPostgresDB   = "netbird"
)

// cnpgAppSecretPollInterval / cnpgAppSecretPollTimeout govern the
// wait for CNPG to materialise the netbird-pgsql-app Secret after
// netbird's Cluster CR lands. CNPG typically creates the Secret on
// its first reconcile (~5–30s); five minutes is a generous safety
// net for a slow operator startup or first-time image pull.
const (
	cnpgAppSecretPollInterval = 5 * time.Second
	cnpgAppSecretPollTimeout  = 5 * time.Minute
)

// waitAndPatchNetBirdPostgresDSN polls until CNPG has created the
// netbird-pgsql-app Secret in the netbird namespace, then patches
// netbird's Secret with the DSN built from it.
//
// Called from netbird's AfterSync hook so the patch happens with
// the rest of netbird's wiring rather than trailing as a separate
// post-sync step. The poll is necessary because netbird's ArgoCD
// sync returns as soon as the Cluster CR is applied — CNPG still
// has to reconcile that CR into running pods + the *-app Secret,
// a few seconds later.
func waitAndPatchNetBirdPostgresDSN(ctx context.Context, clusterClient client.Client) error {
	err := wait.PollUntilContextTimeout(ctx,
		cnpgAppSecretPollInterval, cnpgAppSecretPollTimeout, true,
		func(ctx context.Context) (bool, error) {
			cur := &coreV1.Secret{}
			if err := clusterClient.Get(ctx,
				types.NamespacedName{Namespace: constants.NamespaceNetBird, Name: netBirdPostgresAppSecret},
				cur,
			); err != nil {
				if k8sAPIErrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		},
	)
	if err != nil {
		return fmt.Errorf("waiting for CNPG app Secret %s/%s: %w",
			constants.NamespaceNetBird, netBirdPostgresAppSecret, err)
	}
	return patchNetBirdPostgresDSN(ctx, clusterClient)
}

// patchNetBirdPostgresDSN reads the CNPG-generated app credentials
// Secret in the netbird namespace, builds a postgres connection
// string from it, and patches the netbird Secret's postgresDSN key
// with the result.
//
// Why patch live instead of regenerating the SealedSecret in git:
// CNPG generates the postgres password on its first sync (in-cluster);
// kubeaid-cli has no way to know the password at template-render
// time, so the initial SealedSecret renders postgresDSN as "". This
// step closes the loop after CNPG is up. On re-runs templates.go's
// read-or-generate path picks up the patched value from the live
// Secret and re-renders the SealedSecret with the correct DSN, so
// the patched value isn't fragile to re-syncs.
//
// No-op when the CNPG app secret hasn't appeared yet (e.g.
// netbird-pgsql Cluster CR not yet rendered into kubeaid-config) —
// logged but not fatal so an operator can rerun bootstrap once the
// Cluster CR is in place.
func patchNetBirdPostgresDSN(ctx context.Context, clusterClient client.Client) error {
	appSecret := &coreV1.Secret{}
	err := clusterClient.Get(ctx,
		types.NamespacedName{Namespace: constants.NamespaceNetBird, Name: netBirdPostgresAppSecret},
		appSecret,
	)
	if k8sAPIErrors.IsNotFound(err) {
		return fmt.Errorf(
			"CNPG app Secret %s/%s not found — is the netbird-pgsql Cluster CR rendered into kubeaid-config?",
			constants.NamespaceNetBird, netBirdPostgresAppSecret,
		)
	}
	if err != nil {
		return fmt.Errorf("reading CNPG app Secret %s/%s: %w",
			constants.NamespaceNetBird, netBirdPostgresAppSecret, err)
	}

	username, ok := appSecret.Data["username"]
	if !ok || len(username) == 0 {
		return fmt.Errorf("CNPG app Secret %s/%s missing 'username' key",
			constants.NamespaceNetBird, netBirdPostgresAppSecret)
	}
	password, ok := appSecret.Data["password"]
	if !ok || len(password) == 0 {
		return fmt.Errorf("CNPG app Secret %s/%s missing 'password' key",
			constants.NamespaceNetBird, netBirdPostgresAppSecret)
	}

	dsn := buildPostgresDSN(string(username), string(password))

	netbirdSecret := &coreV1.Secret{}
	if err := clusterClient.Get(ctx,
		types.NamespacedName{Namespace: constants.NamespaceNetBird, Name: constants.SecretNameNetBird},
		netbirdSecret,
	); err != nil {
		return fmt.Errorf("reading netbird Secret: %w", err)
	}

	if netbirdSecret.Data == nil {
		netbirdSecret.Data = map[string][]byte{}
	}
	netbirdSecret.Data[constants.SecretKeyNetBirdPostgresDSN] = []byte(dsn)

	if err := clusterClient.Update(ctx, netbirdSecret); err != nil {
		return fmt.Errorf("patching netbird Secret with postgres DSN: %w", err)
	}
	return nil
}

// buildPostgresDSN composes a libpq URI from CNPG-supplied creds.
// url.UserPassword handles the percent-encoding so any special
// chars CNPG happens to generate in the password don't break the
// DSN parser.
func buildPostgresDSN(username, password string) string {
	u := &url.URL{
		Scheme: "postgresql",
		User:   url.UserPassword(username, password),
		Host:   netBirdPostgresHost + ":" + netBirdPostgresPort,
		Path:   "/" + netBirdPostgresDB,
	}
	return u.String()
}
