// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

// Package klist reads and merges cluster registry YAML files from a local
// klist clone. The layout is:
//
//	clusters/<customerid>/_customer.yaml   (optional, shared OIDC defaults)
//	clusters/<customerid>/<clustername>.yaml (required, per-cluster config)
//
// The cluster file always wins on field conflict during a shallow merge.
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

// OIDCConfig holds OIDC settings for a cluster. Fields may be populated from
// _customer.yaml (defaults) or the cluster YAML (per-cluster overrides), with
// the cluster YAML winning on conflict.
type OIDCConfig struct {
	// IssuerURL is the Keycloak realm URL used by kube-apiserver.
	IssuerURL string `yaml:"issuerUrl"`
	// ClientID is the Keycloak client ID for this specific cluster.
	ClientID string `yaml:"clientId"`
	// GroupsClaim is the JWT claim kube-apiserver reads for RBAC groups.
	// Defaults to "groups" when absent in both customer and cluster YAML.
	GroupsClaim string `yaml:"groupsClaim"`
	// UsernameClaim is the JWT claim kube-apiserver maps to the user identity.
	// Defaults to "email" when absent in both customer and cluster YAML.
	UsernameClaim string `yaml:"usernameClaim"`
}

// CustomerDefaults holds optional per-customer defaults from _customer.yaml.
type CustomerDefaults struct {
	Customer    string     `yaml:"customer"`
	DisplayName string     `yaml:"displayName"`
	OIDC        OIDCConfig `yaml:"oidc"`
}

// ClusterConfig holds the merged per-cluster configuration. Required fields
// (Name, Server, CABundle, OIDC.IssuerURL, OIDC.ClientID) must be non-empty
// after merging; Validate enforces this.
type ClusterConfig struct {
	Name          string     `yaml:"name"`
	Server        string     `yaml:"server"`
	CABundle      string     `yaml:"caBundle"`
	OIDC          OIDCConfig `yaml:"oidc"`
	AllowedGroups []string   `yaml:"allowedGroups"`
}

// Validate returns an error listing every required field that is empty after
// merging. The error message names the missing fields so the user knows
// exactly what to add.
func (c *ClusterConfig) Validate() error {
	var missing []string

	if c.Name == "" {
		missing = append(missing, "name")
	}

	if c.Server == "" {
		missing = append(missing, "server")
	}

	if c.CABundle == "" {
		missing = append(missing, "caBundle")
	}

	if c.OIDC.IssuerURL == "" {
		missing = append(missing, "oidc.issuerUrl")
	}

	if c.OIDC.ClientID == "" {
		missing = append(missing, "oidc.clientId")
	}

	if len(missing) > 0 {
		return fmt.Errorf("cluster config missing required fields: %v", missing)
	}

	return nil
}

// Load reads the optional _customer.yaml and the required
// <clustername>.yaml from registryPath/clusters/<customerid>/, then returns a
// shallow-merged ClusterConfig with per-cluster values winning on conflict.
//
// GroupsClaim defaults to "groups" and UsernameClaim to "email" when both
// customer and cluster YAMLs omit them.
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

// merge applies customer defaults as the base and overlays non-zero cluster
// fields on top. Cluster values always win.
func merge(customer CustomerDefaults, cluster ClusterConfig) *ClusterConfig {
	out := cluster

	// OIDC: fill from customer when cluster leaves the field empty.
	if out.OIDC.IssuerURL == "" {
		out.OIDC.IssuerURL = customer.OIDC.IssuerURL
	}

	if out.OIDC.GroupsClaim == "" {
		out.OIDC.GroupsClaim = customer.OIDC.GroupsClaim
	}

	if out.OIDC.UsernameClaim == "" {
		out.OIDC.UsernameClaim = customer.OIDC.UsernameClaim
	}

	// Apply built-in defaults for optional OIDC claims.
	if out.OIDC.GroupsClaim == "" {
		out.OIDC.GroupsClaim = "groups"
	}

	if out.OIDC.UsernameClaim == "" {
		out.OIDC.UsernameClaim = "email"
	}

	return &out
}
