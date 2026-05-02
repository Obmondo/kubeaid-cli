// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package klist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// GlobalConfigFile is the optional top-level YAML in the klist clone
	// that declares deployment-wide defaults (NetBird server, peer
	// naming convention).
	GlobalConfigFile = "global.yaml"

	// DefaultClusterPeerPrefix is what AccessibleClusters expects in
	// front of a peer FQDN to recognise it as a cluster.
	DefaultClusterPeerPrefix = "k8s-"
	// DefaultClusterPeerSuffix is the FQDN suffix matching the example
	// in docs/netbird-vpn-architecture.md. Real deployments usually
	// override this in global.yaml (Obmondo's, for instance, ends in
	// `.netbird.selfhosted`).
	DefaultClusterPeerSuffix = ".netbird"
)

// GlobalConfig is the parsed shape of klist/global.yaml.
type GlobalConfig struct {
	NetBird NetBirdSettings `yaml:"netbird"`
}

// NetBirdSettings declares the NetBird deployment that hosts a cluster's
// kube-api endpoint. Per-customer files may override individual fields;
// missing fields fall back to the global file or to baked-in defaults.
//
// ClusterPeerPrefix and ClusterPeerSuffix are *string so we can tell
// "field omitted" (use default) apart from "field explicitly empty"
// (use empty). A plain string can't carry that distinction — the zero
// value is indistinguishable from `field: ""`.
type NetBirdSettings struct {
	// ManagementURL is the expected `management.url` reported by the
	// local netbird daemon. Used for sanity-checking; mismatch is a
	// warning, not a failure.
	ManagementURL string `yaml:"managementUrl"`
	// ClusterPeerPrefix and ClusterPeerSuffix bracket the cluster name
	// in the NetBird peer FQDN, e.g. prefix "k8s-" + cluster name
	// "staging" + suffix ".netbird.selfhosted". Use the Prefix() and
	// Suffix() accessors instead of dereferencing — they apply
	// baked-in defaults when the field is nil.
	ClusterPeerPrefix *string `yaml:"clusterPeerPrefix,omitempty"`
	ClusterPeerSuffix *string `yaml:"clusterPeerSuffix,omitempty"`
}

// Prefix returns the configured ClusterPeerPrefix, or the baked-in
// default when the field was omitted from YAML. Explicit empty string
// in YAML is preserved as "".
func (n *NetBirdSettings) Prefix() string {
	if n.ClusterPeerPrefix == nil {
		return DefaultClusterPeerPrefix
	}

	return *n.ClusterPeerPrefix
}

// Suffix is the same as Prefix, for ClusterPeerSuffix.
func (n *NetBirdSettings) Suffix() string {
	if n.ClusterPeerSuffix == nil {
		return DefaultClusterPeerSuffix
	}

	return *n.ClusterPeerSuffix
}

// LoadGlobal reads registryPath/global.yaml (optional). If the file is
// missing, returns a GlobalConfig with everything unset — call Prefix()
// and Suffix() on the embedded NetBirdSettings to get values that fall
// back to baked-in defaults.
func LoadGlobal(registryPath string) (*GlobalConfig, error) {
	out := &GlobalConfig{}

	path := filepath.Join(registryPath, GlobalConfigFile)

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// File optional — fall through with zero value (Prefix()/Suffix()
		// will return the baked-in defaults).
	case err != nil:
		return nil, fmt.Errorf("reading %q: %w", path, err)
	default:
		if yamlErr := yaml.Unmarshal(data, out); yamlErr != nil {
			return nil, fmt.Errorf("parsing %q: %w", path, yamlErr)
		}
	}

	return out, nil
}

// ClusterRef points at one entry in the klist registry: the directory
// (customer) it belongs to, the cluster's name (file basename without
// the .yaml extension), and the path to the YAML on disk.
type ClusterRef struct {
	Customer    string
	ClusterName string
	YAMLPath    string
}

// ListClusters walks registryPath/clusters and returns every per-cluster
// YAML it finds, sorted (customer, cluster) for stable output. Files
// starting with "_" (e.g. "_customer.yaml") are skipped — they're
// per-customer defaults, not clusters.
func ListClusters(registryPath string) ([]ClusterRef, error) {
	root := filepath.Join(registryPath, "clusters")

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", root, err)
	}

	var refs []ClusterRef

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		customer := entry.Name()

		customerDir := filepath.Join(root, customer)

		files, err := os.ReadDir(customerDir)
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", customerDir, err)
		}

		for _, f := range files {
			if f.IsDir() {
				continue
			}

			name := f.Name()
			if !strings.HasSuffix(name, ".yaml") {
				continue
			}
			// `_customer.yaml` and any other `_*.yaml` are defaults files,
			// not clusters.
			if strings.HasPrefix(name, "_") {
				continue
			}

			refs = append(refs, ClusterRef{
				Customer:    customer,
				ClusterName: strings.TrimSuffix(name, ".yaml"),
				YAMLPath:    filepath.Join(customerDir, name),
			})
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Customer != refs[j].Customer {
			return refs[i].Customer < refs[j].Customer
		}

		return refs[i].ClusterName < refs[j].ClusterName
	})

	return refs, nil
}
