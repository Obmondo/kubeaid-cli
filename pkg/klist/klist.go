// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

// Package klist reads and merges cluster registry YAML files from a local
// klist clone. The layout is:
//
//	clusters/<customerid>/_customer.yaml   (optional, shared OIDC issuers)
//	clusters/<customerid>/<clustername>.yaml (required, per-cluster config)
//
// A cluster lists every OIDC issuer its kube-apiserver trusts. Customer-level
// issuers from _customer.yaml are prepended to each cluster's own issuer list;
// the cluster file wins on every other (scalar) field.
package klist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrClusterNotFound is the sentinel returned (wrapped) by Load when
// the requested clusterName.yaml does not exist in the registry. Use
// errors.Is to detect it — callers then have the option to surface a
// "did you mean …" list of available clusters.
var ErrClusterNotFound = errors.New("cluster file not found in klist registry")

// OIDCIssuer is one OpenID Connect issuer a cluster's kube-apiserver trusts.
// A cluster lists every issuer a user may authenticate through; `login`
// prompts the user to choose when there is more than one (typically the
// customer's own Keycloak and Obmondo's central SRE Keycloak).
type OIDCIssuer struct {
	// Name labels the issuer in the interactive picker and selects it via
	// `login --issuer <name>`. Required and unique when a cluster lists more
	// than one issuer; optional for a single-issuer cluster.
	Name string `yaml:"name"`
	// IssuerURL is the Keycloak realm URL kube-apiserver validates JWTs against.
	IssuerURL string `yaml:"issuerUrl"`
	// ClientID is the OIDC client whose tokens kube-apiserver accepts (the
	// token's `aud`). Per-issuer — the customer and Obmondo entries differ.
	ClientID string `yaml:"clientId"`
	// GroupsClaim is the JWT claim kube-apiserver reads for RBAC groups.
	// Defaults to "groups" when omitted.
	GroupsClaim string `yaml:"groupsClaim"`
	// UsernameClaim is the JWT claim kube-apiserver maps to the user identity.
	// Defaults to "email" when omitted.
	UsernameClaim string `yaml:"usernameClaim"`
}

// CustomerDefaults holds optional per-customer defaults from _customer.yaml.
// Its OIDC issuers (if any) are shared across all of the customer's clusters
// and are prepended to each cluster's own issuer list during merge.
type CustomerDefaults struct {
	Customer    string       `yaml:"customer"`
	DisplayName string       `yaml:"displayName"`
	OIDC        []OIDCIssuer `yaml:"oidc"`
}

// ClusterConfig holds the merged per-cluster configuration. After merging,
// OIDC holds every issuer the cluster trusts (customer-level issuers first,
// then the cluster's own); Validate enforces the required fields.
type ClusterConfig struct {
	Name          string       `yaml:"name"`
	Server        string       `yaml:"server"`
	CABundle      string       `yaml:"caBundle"`
	OIDC          []OIDCIssuer `yaml:"oidc"`
	AllowedGroups []string     `yaml:"allowedGroups"`
}

// Validate returns an error listing every required field that is empty or
// invalid after merging. The message names each problem so the user knows
// exactly what to fix. It enforces the top-level fields, that at least one
// OIDC issuer is present, that each issuer carries an issuerUrl and clientId,
// and — when the cluster lists more than one issuer — that every issuer has a
// unique name (the picker needs a stable label to select on).
func (c *ClusterConfig) Validate() error {
	var problems []string

	if c.Name == "" {
		problems = append(problems, "name")
	}

	if c.Server == "" {
		problems = append(problems, "server")
	}

	if c.CABundle == "" {
		problems = append(problems, "caBundle")
	}

	if len(c.OIDC) == 0 {
		problems = append(problems, "oidc (at least one issuer required)")
	}

	problems = append(problems, validateIssuers(c.OIDC)...)

	if len(problems) > 0 {
		return fmt.Errorf("cluster config missing/invalid fields: %v", problems)
	}

	return nil
}

// validateIssuers checks that each issuer has the required fields and — when a
// cluster lists more than one — that names are present and unique, so `login`
// can label and select issuers unambiguously. Returns the list of problems
// (empty when every issuer is valid).
func validateIssuers(issuers []OIDCIssuer) []string {
	var problems []string

	requireNames := len(issuers) > 1
	seen := make(map[string]bool, len(issuers))

	for i, issuer := range issuers {
		if issuer.IssuerURL == "" {
			problems = append(problems, fmt.Sprintf("oidc[%d].issuerUrl", i))
		}

		if issuer.ClientID == "" {
			problems = append(problems, fmt.Sprintf("oidc[%d].clientId", i))
		}

		if !requireNames {
			continue
		}

		if issuer.Name == "" {
			problems = append(problems, fmt.Sprintf("oidc[%d].name (required when a cluster has multiple issuers)", i))

			continue
		}

		if seen[issuer.Name] {
			problems = append(problems, fmt.Sprintf("oidc[%d].name %q (duplicate)", i, issuer.Name))
		}

		seen[issuer.Name] = true
	}

	return problems
}

// Load reads the optional _customer.yaml and the required
// <clustername>.yaml from registryPath/clusters/<customerid>/, then returns a
// merged ClusterConfig: customer-level OIDC issuers are prepended to the
// cluster's own, and the cluster file wins on every scalar field.
//
// Each issuer's GroupsClaim defaults to "groups" and UsernameClaim to "email"
// when omitted.
func Load(registryPath, clusterName, customerID string) (*ClusterConfig, error) {
	clusterDir := filepath.Join(registryPath, "clusters", customerID)

	// Load optional customer defaults.
	var customer CustomerDefaults

	customerPath := filepath.Join(clusterDir, "_customer.yaml")

	customerData, err := os.ReadFile(customerPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading customer defaults %q: %w", customerPath, err)
	}

	if err == nil {
		if yamlErr := yaml.Unmarshal(customerData, &customer); yamlErr != nil {
			return nil, fmt.Errorf("parsing customer defaults %q: %w", customerPath, yamlErr)
		}
	}

	// Resolve the cluster file. Identity is the in-YAML `name:` field, so a
	// cluster can be renamed (e.g. to track its NetBird peer FQDN, which is
	// what the interactive picker intersects against) without renaming the
	// file on disk. Falls back to <clusterName>.yaml for registries that
	// don't set `name:`, and so a malformed target file still resolves here
	// and surfaces its parse error below rather than a confusing not-found.
	clusterPath, err := resolveClusterPath(clusterDir, clusterName)
	if err != nil {
		return nil, err
	}

	clusterData, err := os.ReadFile(clusterPath)
	if err != nil {
		return nil, fmt.Errorf("reading cluster config %q: %w", clusterPath, err)
	}

	var cluster ClusterConfig
	if yamlErr := yaml.Unmarshal(clusterData, &cluster); yamlErr != nil {
		return nil, fmt.Errorf("parsing cluster config %q: %w", clusterPath, yamlErr)
	}

	// Shallow merge: customer defaults fill in empty fields; cluster wins on
	// conflict.
	merged := merge(customer, cluster)

	return merged, nil
}

// resolveClusterPath returns the path to the cluster file in clusterDir
// identified by clusterName. It matches on each file's in-YAML `name:`
// field first, then falls back to <clusterName>.yaml. Returns a wrapped
// ErrClusterNotFound when neither resolves. Non-cluster files (`_*.yaml`,
// non-YAML) and files that fail the name probe are skipped during the
// primary match; a malformed <clusterName>.yaml still resolves via the
// filename fallback so the caller surfaces its parse error.
func resolveClusterPath(clusterDir, clusterName string) (string, error) {
	byFilename := filepath.Join(clusterDir, clusterName+".yaml")

	entries, err := os.ReadDir(clusterDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrClusterNotFound, byFilename)
		}

		return "", fmt.Errorf("reading cluster directory %q: %w", clusterDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") || strings.HasPrefix(name, "_") {
			continue
		}

		path := filepath.Join(clusterDir, name)
		if clusterNameOrFallback(path, "") == clusterName {
			return path, nil
		}
	}

	if _, statErr := os.Stat(byFilename); statErr == nil {
		return byFilename, nil
	}

	return "", fmt.Errorf("%w: %s", ErrClusterNotFound, byFilename)
}

// clusterNameOrFallback returns the cluster's `name:` field read from the
// YAML at path, or fallback when the file can't be read, fails to parse, or
// omits `name:`. Used both to resolve a requested cluster by its in-YAML
// identity and to label ClusterRefs in ListClusters.
func clusterNameOrFallback(path, fallback string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}

	var probe struct {
		Name string `yaml:"name"`
	}

	if err := yaml.Unmarshal(data, &probe); err != nil || probe.Name == "" {
		return fallback
	}

	return probe.Name
}

// merge builds the effective cluster config: customer-level issuers are
// prepended to the cluster's own (so a complete issuer shared by all of a
// customer's clusters can live once in _customer.yaml), and each issuer's
// optional claim fields fall back to the built-in defaults. The cluster file
// wins on every scalar field — those are copied verbatim from cluster.
func merge(customer CustomerDefaults, cluster ClusterConfig) *ClusterConfig {
	out := cluster

	// Customer issuers first, then the cluster's own. A fresh slice avoids
	// mutating either input's backing array.
	issuers := make([]OIDCIssuer, 0, len(customer.OIDC)+len(cluster.OIDC))
	issuers = append(issuers, customer.OIDC...)
	issuers = append(issuers, cluster.OIDC...)

	for i := range issuers {
		if issuers[i].GroupsClaim == "" {
			issuers[i].GroupsClaim = "groups"
		}

		if issuers[i].UsernameClaim == "" {
			issuers[i].UsernameClaim = "email"
		}
	}

	out.OIDC = issuers

	return &out
}
