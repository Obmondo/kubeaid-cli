// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/randval"
)

// Rendered files the credential state is read back from, relative to the
// cluster directory.
const (
	securityOperationsAppsFile          = "argocd-apps/templates/security-operations.yaml"
	securityOperationsCentralValuesFile = "argocd-apps/values-security-operations.yaml"
)

// credentialsFromClusterDir builds the Wazuh credentials of a render without
// secrets.yaml. The state lives in the cluster directory: the bcrypt hashes
// in the rendered values, the passwords only inside the sealed Secrets. A
// release (the central search, or one tenant) whose sealed files and hashes
// are all present is kept: its hashes are reused and its sealed files are not
// rewritten, so its passwords are never needed. Any other release gets fresh
// credentials, and all its Secrets are sealed anew.
//
// The manager cluster key (SealOnce) does not decide whether a release is
// kept. A kept tenant without its sealed cluster key gets one: the key a
// render before the move to wazuh.clusterKeySecret left in plaintext in the
// Application (so nothing changes for the manager), else a fresh one.
//
// Returns the credentials and the sealed files (relative to clusterDir) that
// stay as they are.
func credentialsFromClusterDir(clusterDir string) (*config.SecurityOperationsCredentials, map[string]bool, error) {
	central, tenants, err := readRenderedCredentialState(clusterDir)
	if err != nil {
		return nil, nil, err
	}

	exists := func(relativePath string) bool {
		_, err := os.Stat(path.Join(clusterDir, relativePath))
		return err == nil
	}

	sealedPresent := map[string]bool{}
	for _, secret := range securityOperationsSecretFiles() {
		if secret.SealOnce {
			continue
		}
		ns := secret.Data.Namespace
		if _, seen := sealedPresent[ns]; !seen {
			sealedPresent[ns] = true
		}
		if !exists(secret.RelativePath) {
			sealedPresent[ns] = false
		}
	}

	creds := &config.SecurityOperationsCredentials{
		Tenants: map[string]config.SecurityOperationsWazuhCredentials{},
	}
	keptNamespaces := map[string]bool{}

	resolve := func(namespace string, state config.SecurityOperationsWazuhCredentials, tenant bool) (
		config.SecurityOperationsWazuhCredentials, error,
	) {
		complete := state.IndexerPasswordHash != "" && state.DashboardPasswordHash != ""
		if !complete || !sealedPresent[namespace] {
			return parser.NewWazuhCredentials(tenant)
		}
		keptNamespaces[namespace] = true
		clusterKeyFile := path.Join("sealed-secrets", namespace, constants.SecretNameWazuhClusterKey+".yaml")
		if tenant && state.ClusterKey == "" && !exists(clusterKeyFile) {
			if state.ClusterKey, err = randval.Password(); err != nil {
				return state, err
			}
		}
		return state, nil
	}

	if creds.Central, err = resolve(constants.NamespaceSecurityOperations, central, false); err != nil {
		return nil, nil, err
	}
	for _, tenant := range config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants {
		ns := constants.SecurityOperationsTenantNamespacePrefix + tenant.Code
		c, err := resolve(ns, tenants[tenant.Code], true)
		if err != nil {
			return nil, nil, err
		}
		creds.Tenants[tenant.Code] = c
	}

	keep := map[string]bool{}
	for _, secret := range securityOperationsSecretFiles() {
		if keptNamespaces[secret.Data.Namespace] && (!secret.SealOnce || exists(secret.RelativePath)) {
			keep[secret.RelativePath] = true
		}
	}
	return creds, keep, nil
}

// readRenderedCredentialState reads the password hashes from a previous
// render: the central ones from the central values file, a tenant's from the
// helm.valuesObject of its wazuh-<code> Application, with the cluster key a
// render before wazuh.clusterKeySecret put there (empty since). Missing files
// mean a first render and yield empty state.
func readRenderedCredentialState(clusterDir string) (
	config.SecurityOperationsWazuhCredentials, map[string]config.SecurityOperationsWazuhCredentials, error,
) {
	var central config.SecurityOperationsWazuhCredentials
	tenants := map[string]config.SecurityOperationsWazuhCredentials{}

	raw, err := os.ReadFile(path.Join(clusterDir, securityOperationsCentralValuesFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return central, nil, err
	default:
		var values map[string]any
		if err := yaml.Unmarshal(raw, &values); err != nil {
			return central, nil, fmt.Errorf("parsing %s: %w", securityOperationsCentralValuesFile, err)
		}
		central.IndexerPasswordHash = stringAt(values, "wazuh", "wazuh", "indexer", "cred", "passwordHash")
		central.DashboardPasswordHash = stringAt(values, "wazuh", "wazuh", "dashboard", "cred", "passwordHash")
	}

	raw, err = os.ReadFile(path.Join(clusterDir, securityOperationsAppsFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return central, tenants, nil
	case err != nil:
		return central, nil, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var app map[string]any
		if err := decoder.Decode(&app); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return central, nil, fmt.Errorf("parsing %s: %w", securityOperationsAppsFile, err)
		}
		name := stringAt(app, "metadata", "name")
		code, ok := strings.CutPrefix(name, constants.SecurityOperationsTenantNamespacePrefix)
		if !ok || code == "" {
			continue
		}
		sources, _ := valueAt(app, "spec", "sources").([]any)
		for _, source := range sources {
			values, ok := valueAt(source, "helm", "valuesObject").(map[string]any)
			if !ok {
				continue
			}
			tenants[code] = config.SecurityOperationsWazuhCredentials{
				IndexerPasswordHash:   stringAt(values, "wazuh", "indexer", "cred", "passwordHash"),
				DashboardPasswordHash: stringAt(values, "wazuh", "dashboard", "cred", "passwordHash"),
				ClusterKey:            stringAt(values, "wazuh", "wazuh", "key"),
			}
		}
	}
	return central, tenants, nil
}

// valueAt walks nested maps; nil when a key is missing.
func valueAt(v any, keys ...string) any {
	for _, key := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return v
}

// stringAt is valueAt for a string leaf; "" otherwise.
func stringAt(v any, keys ...string) string {
	s, _ := valueAt(v, keys...).(string)
	return s
}

// mispRedisSecretFile returns MISP's Valkey password Secret
// (sealed-secrets/security-operations/misp-redis.yaml) to seal, or nil when
// its sealed file exists already: the password then lives only in there and
// is never rotated by a render.
func mispRedisSecretFile(clusterDir string) (*securityOperationsSecret, error) {
	if !config.SecurityOperationsEnabled() {
		return nil, nil
	}
	ns := constants.NamespaceSecurityOperations
	relativePath := path.Join("sealed-secrets", ns, constants.SecretNameMISPRedis+".yaml")
	if _, err := os.Stat(path.Join(clusterDir, relativePath)); err == nil {
		return nil, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	password, err := mispRedisPasswordFromClusterDir(clusterDir)
	if err != nil {
		return nil, err
	}
	return &securityOperationsSecret{
		TemplateName: constants.TemplateNameMISPRedis,
		RelativePath: relativePath,
		Data: securityOperationsSecretData{
			Name:      constants.SecretNameMISPRedis,
			Namespace: ns,
			Password:  password,
		},
		SealOnce: true,
	}, nil
}

// mispRedisPasswordFromClusterDir returns the Valkey password for a new
// misp-redis Secret: the one a render before the move to that Secret left in
// plaintext (misp.misp.env.redisPassword in the central values), so running
// pods keep working, else a fresh one (also instead of the chart default).
func mispRedisPasswordFromClusterDir(clusterDir string) (string, error) {
	raw, err := os.ReadFile(path.Join(clusterDir, securityOperationsCentralValuesFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err == nil {
		var values map[string]any
		if err := yaml.Unmarshal(raw, &values); err != nil {
			return "", fmt.Errorf("parsing %s: %w", securityOperationsCentralValuesFile, err)
		}
		if pw := stringAt(values, "misp", "misp", "env", "redisPassword"); pw != "" && pw != mispChartDefaultRedisPassword {
			return pw, nil
		}
	}
	return randval.Password()
}

// mispChartDefaultRedisPassword is the misp chart's placeholder.
const mispChartDefaultRedisPassword = "change-me"
