// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package content rolls the kubesoc detection content package (KubeAid
// argocd-helm-charts/kubesoc-content) out to the running tools: Wazuh
// rules, decoders, CDB lists and agent group configs through each tenant
// manager's API (canary first, validated, hot-reloaded, rolled back on
// failure), and Velociraptor artifacts through VQL artifact_set. The
// last content that went in cleanly is kept per target in a Secret.
package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Files of the package root.
const (
	versionFile  = "VERSION"
	manifestFile = "manifest.json"
	// flatSep stands for "/" in ConfigMap keys, which cannot hold one:
	// wazuh__rules__x.xml is wazuh/rules/x.xml.
	flatSep = "__"
)

// Kinds of Wazuh content, as the prefix of a Set key ("rules/x.xml").
const (
	KindRules       = "rules"
	KindDecoders    = "decoders"
	KindLists       = "lists"
	KindAgentGroups = "agent-groups"
)

var (
	xmlName   = regexp.MustCompile(`^[-\w.]+\.xml$`)
	listName  = regexp.MustCompile(`^[-\w]+$`) // the API's list file name format
	groupName = regexp.MustCompile(`^[-\w.]+$`)
	// Files the wazuh chart writes at every start; content must not own them.
	chartOwned = map[string]bool{"rules/local_rules.xml": true, "decoders/local_decoder.xml": true}
)

// Manifest is the package's manifest.json, rendered by the chart from its
// values: what goes to which tenant.
type Manifest struct {
	// Exclude lists package paths (wazuh/rules/x.xml) no tenant gets.
	Exclude []string `json:"exclude,omitempty"`
	// Optional lists package paths only tenants that include them get.
	Optional []string `json:"optional,omitempty"`
	// Tenants holds per-tenant include / exclude lists by tenant code.
	Tenants map[string]TenantManifest `json:"tenants,omitempty"`
}

// TenantManifest selects content for one tenant.
type TenantManifest struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// Artifact is one Velociraptor artifact definition.
type Artifact struct {
	Name string
	// Source is the file it came from.
	Source string
	Raw    []byte
}

// Bundle is the loaded content package.
type Bundle struct {
	Version  string
	Manifest Manifest
	// Wazuh maps kind/name (rules/x.xml, lists/y, agent-groups/g) to content.
	Wazuh map[string][]byte
	// Overlays maps a tenant code to its own kind/name files, which are
	// added to or replace the shared ones.
	Overlays  map[string]map[string][]byte
	Artifacts map[string]Artifact
}

// Load reads the package at dir and the Velociraptor artifact files
// (*.yaml) of artifactDirs. dir is either a checkout of the chart
// directory or the kubesoc-content ConfigMap mount, whose file names
// carry "__" for "/".
func Load(dir string, artifactDirs []string) (*Bundle, error) {
	b := &Bundle{Wazuh: map[string][]byte{}, Overlays: map[string]map[string][]byte{}, Artifacts: map[string]Artifact{}}
	files, err := readTree(dir)
	if err != nil {
		return nil, err
	}
	var errs []error
	for p, data := range files {
		if err := b.add(p, data, filepath.Join(dir, p)); err != nil {
			errs = append(errs, err)
		}
	}
	for _, d := range artifactDirs {
		extra, err := readTree(d)
		if err != nil {
			return nil, err
		}
		for p, data := range extra {
			if strings.Contains(p, "/") || !strings.HasSuffix(p, ".yaml") {
				continue
			}
			if err := b.addArtifact(data, filepath.Join(d, p)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if b.Version == "" {
		errs = append(errs, fmt.Errorf("%s: no %s file", dir, versionFile))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b, nil
}

// readTree returns every regular file under dir by slash path, skipping
// dot entries (a ConfigMap mount's ..data) and unflattening "__".
func readTree(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != dir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		st, err := os.Stat(path) // follows the mount's symlinks
		if err != nil || !st.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // a read-only ConfigMap mount or a checkout, not untrusted input
		if err != nil {
			return err
		}
		out[strings.ReplaceAll(filepath.ToSlash(rel), flatSep, "/")] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading content %s: %w", dir, err)
	}
	return out, nil
}

// add files one package path; paths outside the content tree (chart
// files, tests, docs) are ignored.
func (b *Bundle) add(p string, data []byte, source string) error {
	switch {
	case p == versionFile:
		b.Version = strings.TrimSpace(string(data))
		return nil
	case p == manifestFile:
		if err := json.Unmarshal(data, &b.Manifest); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		return nil
	case strings.HasPrefix(p, "velociraptor/artifacts/") && strings.HasSuffix(p, ".yaml") && strings.Count(p, "/") == 2:
		return b.addArtifact(data, source)
	case strings.HasPrefix(p, "wazuh/"):
		key, ok, err := wazuhKey(strings.TrimPrefix(p, "wazuh/"))
		if ok {
			b.Wazuh[key] = data
		}
		return wrap(source, err)
	case strings.HasPrefix(p, "tenants/"):
		parts := strings.SplitN(p, "/", 4) // tenants/<code>/wazuh/<rest>
		if len(parts) < 4 || parts[2] != "wazuh" {
			return nil
		}
		key, ok, err := wazuhKey(parts[3])
		if ok {
			if b.Overlays[parts[1]] == nil {
				b.Overlays[parts[1]] = map[string][]byte{}
			}
			b.Overlays[parts[1]][key] = data
		}
		return wrap(source, err)
	}
	return nil
}

func wrap(source string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	return nil
}

// wazuhKey maps a path under wazuh/ to a Set key, checking the file
// name against what the Wazuh API accepts. ok is false for files that
// are not content (a README next to the rules).
func wazuhKey(rel string) (string, bool, error) {
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 2 && (parts[0] == KindRules || parts[0] == KindDecoders):
		if !strings.HasSuffix(parts[1], ".xml") {
			return "", false, nil
		}
		if !xmlName.MatchString(parts[1]) {
			return "", false, fmt.Errorf("file name %q is not a valid Wazuh ruleset file name", parts[1])
		}
		if chartOwned[rel] {
			return "", false, fmt.Errorf("%s is written by the wazuh chart at every start; name the file differently", rel)
		}
		return rel, true, nil
	case len(parts) == 2 && parts[0] == KindLists:
		if strings.Contains(parts[1], ".") {
			return "", false, nil // README.md, and never a list: the API refuses dots
		}
		if !listName.MatchString(parts[1]) {
			return "", false, fmt.Errorf("list name %q must match %s", parts[1], listName)
		}
		return rel, true, nil
	case len(parts) == 3 && parts[0] == KindAgentGroups && parts[2] == "agent.conf":
		if !groupName.MatchString(parts[1]) {
			return "", false, fmt.Errorf("agent group name %q must match %s", parts[1], groupName)
		}
		return KindAgentGroups + "/" + parts[1], true, nil
	}
	return "", false, nil
}

func (b *Bundle) addArtifact(data []byte, source string) error {
	var head struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	if head.Name == "" {
		return fmt.Errorf("%s: artifact has no name", source)
	}
	if prev, ok := b.Artifacts[head.Name]; ok {
		return fmt.Errorf("artifact %s is defined in both %s and %s", head.Name, prev.Source, source)
	}
	b.Artifacts[head.Name] = Artifact{Name: head.Name, Source: source, Raw: data}
	return nil
}

// Set is the Wazuh content one tenant gets.
type Set struct {
	Version string
	// Hash is over every file's key and content.
	Hash string
	// Files maps kind/name to content (agent groups as agent-groups/<group>).
	Files map[string][]byte
}

// ForTenant selects the tenant's content: the shared files less the
// global and tenant excludes, optional files the tenant includes, and
// the tenant's own overlay files on top.
func (b *Bundle) ForTenant(code string) Set {
	tm := b.Manifest.Tenants[code]
	drop := map[string]bool{}
	for _, p := range b.Manifest.Exclude {
		drop[setKey(p)] = true
	}
	for _, p := range tm.Exclude {
		drop[setKey(p)] = true
	}
	optional := map[string]bool{}
	for _, p := range b.Manifest.Optional {
		optional[setKey(p)] = true
	}
	for _, p := range tm.Include {
		delete(optional, setKey(p))
	}
	files := map[string][]byte{}
	for k, v := range b.Wazuh {
		if !drop[k] && !optional[k] {
			files[k] = v
		}
	}
	for k, v := range b.Overlays[code] {
		files[k] = v
	}
	return Set{Version: b.Version, Hash: hashFiles(files), Files: files}
}

// setKey maps a manifest path (wazuh/rules/x.xml or rules/x.xml,
// wazuh/agent-groups/g/agent.conf) to a Set key.
func setKey(p string) string {
	p = strings.TrimPrefix(p, "wazuh/")
	if strings.HasPrefix(p, KindAgentGroups+"/") {
		p = strings.TrimSuffix(p, "/agent.conf")
	}
	return p
}

func hashFiles(files map[string][]byte) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(files[k])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// splitKey returns the kind and name of a Set key.
func splitKey(key string) (string, string) {
	kind, name, _ := strings.Cut(key, "/")
	return kind, name
}
