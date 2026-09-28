// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package siemctl holds the lifecycle logic behind the `kubeaid-cli siem`
// commands (init, apply, status, upgrade, tenant, backup, restore, enroll,
// preflight, quickstart, bundle). The cobra wiring lives in
// cmd/kubeaid-core/root/siem; everything here is testable without a cluster.
package siemctl

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// GeneralConfigFile is a general.yaml opened for editing its
// cluster.securityOperations block. Everything outside that block (comments
// included) is written back unchanged; the block itself is re-encoded, so
// comments inside it are lost.
type GeneralConfigFile struct {
	Path string
	doc  yaml.Node
}

// OpenGeneralConfig reads path. A missing file yields an empty document, so
// `siem init` can create one.
func OpenGeneralConfig(path string) (*GeneralConfigFile, error) {
	f := &GeneralConfigFile{Path: path}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		f.doc = emptyDocument()
		return f, nil
	case err != nil:
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &f.doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if f.doc.Kind == 0 {
		f.doc = emptyDocument()
	}
	if len(f.doc.Content) != 1 || f.doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: the top level must be a mapping", path)
	}
	return f, nil
}

// YAML tags of the nodes this writes.
const (
	tagMap = "!!map"
	tagStr = "!!str"
)

// emptyDocument is a document holding an empty mapping.
func emptyDocument() yaml.Node {
	return yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: tagMap}}}
}

// Exists reports whether the file was read from disk (not created empty).
func (f *GeneralConfigFile) Exists() bool {
	_, err := os.Stat(f.Path)
	return err == nil
}

// SecurityOperations decodes cluster.securityOperations as written, without
// the parser's defaults. Nil when the block is absent.
func (f *GeneralConfigFile) SecurityOperations() (*config.SecurityOperationsConfig, error) {
	node := mappingValue(f.root(), "cluster")
	if node == nil {
		return nil, nil
	}
	node = mappingValue(node, "securityOperations")
	if node == nil {
		return nil, nil
	}
	cfg := &config.SecurityOperationsConfig{}
	if err := node.Decode(cfg); err != nil {
		return nil, fmt.Errorf("decoding cluster.securityOperations: %w", err)
	}
	return cfg, nil
}

// SetSecurityOperations replaces (or adds) cluster.securityOperations.
func (f *GeneralConfigFile) SetSecurityOperations(cfg *config.SecurityOperationsConfig) error {
	var node yaml.Node
	if err := node.Encode(cfg); err != nil {
		return fmt.Errorf("encoding cluster.securityOperations: %w", err)
	}
	pruneZero(&node, "")
	cluster := ensureMapping(f.root(), "cluster")
	setMappingValue(cluster, "securityOperations", &node)
	return nil
}

// SetString sets a scalar at a dotted path of mapping keys (e.g.
// "forkURLs.kubeaid.url"), creating the mappings in between.
func (f *GeneralConfigFile) SetString(value string, keys ...string) {
	m := f.root()
	for _, k := range keys[:len(keys)-1] {
		m = ensureMapping(m, k)
	}
	setMappingValue(m, keys[len(keys)-1], &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: value})
}

// GetString returns the scalar at a dotted path of mapping keys, "" if absent.
func (f *GeneralConfigFile) GetString(keys ...string) string {
	n := f.root()
	for _, k := range keys {
		n = mappingValue(n, k)
		if n == nil {
			return ""
		}
	}
	if n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// Bytes returns the document as YAML (two-space indent).
func (f *GeneralConfigFile) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&f.doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save writes the document back to Path (0600: general.yaml sits next to
// secrets.yaml in the configs directory).
func (f *GeneralConfigFile) Save() error {
	out, err := f.Bytes()
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, out, 0o600)
}

func (f *GeneralConfigFile) root() *yaml.Node { return f.doc.Content[0] }

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			// Keep the comments that sat on the old value.
			value.HeadComment = m.Content[i+1].HeadComment
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: key}, value)
}

func ensureMapping(m *yaml.Node, key string) *yaml.Node {
	if v := mappingValue(m, key); v != nil && v.Kind == yaml.MappingNode {
		return v
	}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap}
	setMappingValue(m, key, v)
	return v
}

// pointerBoolKeys are fields where false differs from absent (nil means
// true), so a false must stay in the file.
var pointerBoolKeys = map[string]bool{"dryRun": true}

// pruneZero drops null, "", 0, false (except pointerBoolKeys) and empty
// collections from an encoded mapping, so the written block only carries
// what the operator set.
func pruneZero(n *yaml.Node, key string) bool {
	switch n.Kind {
	case yaml.DocumentNode, yaml.AliasNode:
		return false
	case yaml.ScalarNode:
		switch {
		case n.Tag == "!!null":
			return true
		case n.Tag == "!!str" && n.Value == "":
			return true
		case n.Tag == "!!int" && n.Value == "0":
			return true
		case n.Tag == "!!bool" && n.Value == "false":
			return !pointerBoolKeys[key]
		}
		return false
	case yaml.MappingNode:
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			if pruneZero(n.Content[i+1], n.Content[i].Value) {
				continue
			}
			kept = append(kept, n.Content[i], n.Content[i+1])
		}
		n.Content = kept
		return len(n.Content) == 0
	case yaml.SequenceNode:
		kept := n.Content[:0]
		for _, c := range n.Content {
			if !pruneZero(c, "") {
				kept = append(kept, c)
			}
		}
		n.Content = kept
		return len(n.Content) == 0
	}
	return false
}

// TenantCodePattern is the umbrella chart's rule for a tenant code.
var TenantCodePattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// AddTenant appends a tenant, refusing a duplicate code or name.
func AddTenant(cfg *config.SecurityOperationsConfig, tenant config.SecurityOperationsTenant) error {
	if !TenantCodePattern.MatchString(tenant.Code) {
		return fmt.Errorf("tenant code must match %s (got %q)", TenantCodePattern, tenant.Code)
	}
	if tenant.Name == "" {
		return errors.New("tenant name is required")
	}
	for _, t := range cfg.Tenants {
		if t.Code == tenant.Code {
			return fmt.Errorf("tenant %q already exists", tenant.Code)
		}
		if t.Name == tenant.Name {
			return fmt.Errorf("tenant name %q is already used by %q", tenant.Name, t.Code)
		}
	}
	cfg.Tenants = append(cfg.Tenants, tenant)
	return nil
}

// RemoveTenant drops the tenant with code; an error when there is none.
func RemoveTenant(cfg *config.SecurityOperationsConfig, code string) (config.SecurityOperationsTenant, error) {
	for i, t := range cfg.Tenants {
		if t.Code == code {
			cfg.Tenants = append(cfg.Tenants[:i], cfg.Tenants[i+1:]...)
			return t, nil
		}
	}
	return config.SecurityOperationsTenant{}, fmt.Errorf("tenant %q is not in cluster.securityOperations.tenants", code)
}
