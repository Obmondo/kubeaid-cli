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
	"slices"
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

// SecurityOperationsRotateAll in SecurityOperationsRenderOptions.Rotate
// rotates the credentials of every release.
const SecurityOperationsRotateAll = "*"

// SecurityOperationsRenderOptions tunes RenderSecurityOperationsWithOptions.
type SecurityOperationsRenderOptions struct {
	// Rotate names the releases whose Wazuh credentials are generated anew
	// even though they exist: security-operations (the central search) and
	// wazuh-<code> (one tenant), or SecurityOperationsRotateAll. Without it a
	// render never replaces the credentials of an existing release.
	Rotate []string
}

// rotateSet validates opts.Rotate against the configured releases.
func (opts SecurityOperationsRenderOptions) rotateSet() (map[string]bool, error) {
	releases := SecurityOperationsApplicationsInSyncOrder()
	set := map[string]bool{}
	for _, name := range opts.Rotate {
		name = strings.TrimSpace(name)
		switch {
		case name == "":
		case name == SecurityOperationsRotateAll:
			for _, release := range releases {
				set[release] = true
			}
		case slices.Contains(releases, name):
			set[name] = true
		default:
			return nil, fmt.Errorf("cannot rotate %q: not a release of this cluster (one of %s)",
				name, strings.Join(releases, ", "))
		}
	}
	return set, nil
}

// credentialsFromClusterDir builds the Wazuh credentials of a render without
// secrets.yaml. The state lives in the cluster directory: the bcrypt hashes
// and cluster keys in the rendered values, the passwords only inside the
// sealed Secrets (in the owned security-operations/sealed-secrets/ directory,
// or still in the legacy sealed-secrets/ one). A release (the central search,
// or one tenant; its name is also its namespace) whose sealed files and
// hashes are all present is kept: its hashes are reused and its sealed files
// are not rewritten, so its passwords are never needed.
//
// A release nothing was rendered for yet gets fresh credentials. A release
// that exists (its Application, values or any sealed file is there) but whose
// state is incomplete is an error listing what is missing: generating new
// passwords for it would silently rotate them. Releases in rotate get fresh
// credentials, and all their Secrets are sealed anew, regardless.
//
// Returns the credentials and the namespaces whose sealed files stay as they are.
func credentialsFromClusterDir(clusterDir string, rotate map[string]bool) (
	*config.SecurityOperationsCredentials, map[string]bool, error,
) {
	state, err := readRenderedCredentialState(clusterDir)
	if err != nil {
		return nil, nil, err
	}

	// Sealed files per namespace: which exist (owned or legacy location),
	// which are missing.
	sealedFound := map[string]bool{}
	sealedMissing := map[string][]string{}
	for _, secret := range securityOperationsSecretFiles() {
		if secret.SealOnce {
			continue
		}
		ns, name := secret.Data.Namespace, secret.Data.Name
		if fileExists(path.Join(clusterDir, securityOperationsSealedSecretPath(ns, name))) ||
			fileExists(path.Join(clusterDir, securityOperationsLegacySealedSecretPath(ns, name))) {
			sealedFound[ns] = true
			continue
		}
		sealedMissing[ns] = append(sealedMissing[ns], securityOperationsSealedSecretPath(ns, name))
	}

	creds := &config.SecurityOperationsCredentials{
		Tenants: map[string]config.SecurityOperationsWazuhCredentials{},
	}
	keep := map[string]bool{}
	var incomplete []string

	resolve := func(release string, current config.SecurityOperationsWazuhCredentials, rendered, tenant bool) (
		config.SecurityOperationsWazuhCredentials, error,
	) {
		if rotate[release] {
			return parser.NewWazuhCredentials(tenant)
		}

		missing := slices.Clone(sealedMissing[release])
		source := securityOperationsCentralValuesFile
		if tenant {
			source = securityOperationsAppsFile + " (Application " + release + ")"
		}
		if current.IndexerPasswordHash == "" {
			missing = append(missing, "indexer password hash in "+source)
		}
		if current.DashboardPasswordHash == "" {
			missing = append(missing, "dashboard password hash in "+source)
		}
		switch {
		case len(missing) == 0:
			keep[release] = true
			return current, nil
		case !rendered && !sealedFound[release]:
			// A new release: nothing to keep, nothing to rotate.
			return parser.NewWazuhCredentials(tenant)
		default:
			incomplete = append(incomplete, fmt.Sprintf("%s:\n    - %s", release, strings.Join(missing, "\n    - ")))
			return current, nil
		}
	}

	central := constants.NamespaceSecurityOperations
	if creds.Central, err = resolve(central, state.central, state.centralRendered, false); err != nil {
		return nil, nil, err
	}
	for _, tenant := range config.ParsedGeneralConfig.Cluster.SecurityOperations.Tenants {
		release := constants.SecurityOperationsTenantNamespacePrefix + tenant.Code
		c, err := resolve(release, state.tenants[tenant.Code], state.tenantsRendered[tenant.Code], true)
		if err != nil {
			return nil, nil, err
		}
		creds.Tenants[tenant.Code] = c
	}

	if len(incomplete) > 0 {
		return nil, nil, &IncompleteSecurityOperationsStateError{Releases: incomplete}
	}
	return creds, keep, nil
}

// IncompleteSecurityOperationsStateError: releases that exist in the cluster
// directory but whose credential state is incomplete. A render refuses to
// replace their credentials unless asked to rotate them.
type IncompleteSecurityOperationsStateError struct {
	// Releases are "<release>:" followed by an indented list of what is missing.
	Releases []string
}

func (e *IncompleteSecurityOperationsStateError) Error() string {
	return "refusing to generate new credentials for releases that already exist " +
		"(that would rotate their passwords); missing:\n  " + strings.Join(e.Releases, "\n  ") +
		"\nRestore the missing files from git, or rotate on purpose with --rotate=<release>"
}

// renderedCredentialState is what a previous render left in the cluster
// directory.
type renderedCredentialState struct {
	central config.SecurityOperationsWazuhCredentials
	// centralRendered: the central values file exists.
	centralRendered bool

	tenants map[string]config.SecurityOperationsWazuhCredentials
	// tenantsRendered: the wazuh-<code> Application exists.
	tenantsRendered map[string]bool
}

// readRenderedCredentialState reads the password hashes and cluster keys from
// a previous render: the central ones from the central values file, a tenant's
// from the helm.valuesObject of its wazuh-<code> Application. Missing files
// mean a first render and yield empty state.
func readRenderedCredentialState(clusterDir string) (renderedCredentialState, error) {
	state := renderedCredentialState{
		tenants:         map[string]config.SecurityOperationsWazuhCredentials{},
		tenantsRendered: map[string]bool{},
	}

	raw, err := os.ReadFile(path.Join(clusterDir, securityOperationsCentralValuesFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return state, err
	default:
		state.centralRendered = true
		var values map[string]any
		if err := yaml.Unmarshal(raw, &values); err != nil {
			return state, fmt.Errorf("parsing %s: %w", securityOperationsCentralValuesFile, err)
		}
		state.central.IndexerPasswordHash = stringAt(values, "wazuh", "wazuh", "indexer", "cred", "passwordHash")
		state.central.DashboardPasswordHash = stringAt(values, "wazuh", "wazuh", "dashboard", "cred", "passwordHash")
	}

	raw, err = os.ReadFile(path.Join(clusterDir, securityOperationsAppsFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return state, nil
	case err != nil:
		return state, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var app map[string]any
		if err := decoder.Decode(&app); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return state, fmt.Errorf("parsing %s: %w", securityOperationsAppsFile, err)
		}
		name := stringAt(app, "metadata", "name")
		code, ok := strings.CutPrefix(name, constants.SecurityOperationsTenantNamespacePrefix)
		if !ok || code == "" {
			continue
		}
		state.tenantsRendered[code] = true
		sources, _ := valueAt(app, "spec", "sources").([]any)
		for _, source := range sources {
			values, ok := valueAt(source, "helm", "valuesObject").(map[string]any)
			if !ok {
				continue
			}
			state.tenants[code] = config.SecurityOperationsWazuhCredentials{
				IndexerPasswordHash:   stringAt(values, "wazuh", "indexer", "cred", "passwordHash"),
				DashboardPasswordHash: stringAt(values, "wazuh", "dashboard", "cred", "passwordHash"),
				ClusterKey:            stringAt(values, "wazuh", "wazuh", "key"),
			}
		}
	}
	return state, nil
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

// mispRedisSecretFile returns MISP's Valkey password Secret
// (sealed-secrets/security-operations/misp-redis.yaml) to seal, or nil when
// its sealed file exists already: the password then lives only in there and
// is never rotated by a render.
func mispRedisSecretFile(clusterDir string) (*securityOperationsSecret, error) {
	if !config.SecurityOperationsEnabled() {
		return nil, nil
	}
	ns := constants.NamespaceSecurityOperations
	relativePath := securityOperationsSealedSecretPath(ns, constants.SecretNameMISPRedis)
	if fileExists(path.Join(clusterDir, relativePath)) ||
		fileExists(path.Join(clusterDir, securityOperationsLegacySealedSecretPath(ns, constants.SecretNameMISPRedis))) {
		return nil, nil
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
