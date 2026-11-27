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
// and cluster keys in the rendered values, the passwords only inside the
// sealed Secrets. A release (the central search, or one tenant) whose sealed
// files and hashes are all present is kept: its hashes are reused and its
// sealed files are not rewritten, so its passwords are never needed. Any other
// release gets fresh credentials, and all its Secrets are sealed anew.
//
// Returns the credentials and the namespaces whose sealed files stay as they are.
func credentialsFromClusterDir(clusterDir string) (*config.SecurityOperationsCredentials, map[string]bool, error) {
	central, tenants, err := readRenderedCredentialState(clusterDir)
	if err != nil {
		return nil, nil, err
	}

	sealedPresent := map[string]bool{}
	for _, secret := range securityOperationsSecretFiles() {
		ns := secret.Data.Namespace
		if _, seen := sealedPresent[ns]; !seen {
			sealedPresent[ns] = true
		}
		if _, err := os.Stat(path.Join(clusterDir, secret.RelativePath)); err != nil {
			sealedPresent[ns] = false
		}
	}

	creds := &config.SecurityOperationsCredentials{
		Tenants: map[string]config.SecurityOperationsWazuhCredentials{},
	}
	keep := map[string]bool{}

	resolve := func(namespace string, state config.SecurityOperationsWazuhCredentials, tenant bool) (
		config.SecurityOperationsWazuhCredentials, error,
	) {
		complete := state.IndexerPasswordHash != "" && state.DashboardPasswordHash != "" &&
			(!tenant || state.ClusterKey != "")
		if complete && sealedPresent[namespace] {
			keep[namespace] = true
			return state, nil
		}
		return parser.NewWazuhCredentials(tenant)
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
	return creds, keep, nil
}

// readRenderedCredentialState reads the password hashes and cluster keys from
// a previous render: the central ones from the central values file, a tenant's
// from the helm.valuesObject of its wazuh-<code> Application. Missing files
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

// mispRedisPasswordFromClusterDir keeps MISP's Valkey password from the
// previous render (misp.misp.env.redisPassword in the central values) and
// generates one on the first render or when it is still the chart default.
// Like the Wazuh hashes, the value lives only in the cluster directory; the
// MISP chart puts it into a ConfigMap either way.
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
