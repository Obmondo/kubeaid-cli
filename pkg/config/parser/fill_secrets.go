// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/renameio"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/randval"
)

// FillMissingSecrets auto-generates and persists random secret
// values into secrets.yaml for fields that are required by the
// cluster's mode but currently empty. Re-runs read the persisted
// values and the SealedSecret render produces byte-identical
// plaintext, so kubeseal doesn't re-encrypt and the operator
// doesn't get noise PRs every time they re-run kubeaid-cli.
//
// Runs after both general.yaml and secrets.yaml are parsed —
// general.yaml tells us which fields are required (cluster type +
// keycloak mode), secrets.yaml tells us what's already filled in.
//
// In-place mutation via yaml.v3 *yaml.Node so the operator's
// existing comments and key ordering survive. Only ADDS missing
// keys; never removes or rewrites existing values.
//
// On any change, the in-memory ParsedSecretsConfig is refreshed
// from the mutated YAML so callers downstream see the freshly-
// generated values.
func FillMissingSecrets(ctx context.Context) error {
	cluster := config.ParsedGeneralConfig.Cluster

	wantNetBird := cluster.Type == constants.ClusterTypeVPN
	wantManagedKeycloak := cluster.Keycloak != nil &&
		cluster.Keycloak.Mode == constants.KeycloakModeManaged

	if !wantNetBird && !wantManagedKeycloak {
		return nil
	}

	secretsPath := config.GetSecretsConfigFilePath()
	raw, err := os.ReadFile(secretsPath)
	if err != nil {
		return fmt.Errorf("reading secrets.yaml: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parsing secrets.yaml as yaml.Node: %w", err)
	}
	docMap, err := documentRootMapping(&root)
	if err != nil {
		return err
	}

	changed := false

	if wantNetBird {
		netbird, err := ensureMappingChild(docMap, "netbird")
		if err != nil {
			return err
		}
		fields := []struct {
			key string
			gen func() (string, error)
		}{
			{"datastoreEncryptionKey", func() (string, error) { return randval.Base64Key(32) }},
			{"relayPassword", randval.Password},
			{"turnPassword", randval.Password},
		}
		for _, f := range fields {
			wrote, err := setScalarIfEmpty(netbird, f.key, f.gen)
			if err != nil {
				return err
			}
			if wrote {
				changed = true
				slog.InfoContext(ctx, "Generated and persisted to secrets.yaml",
					slog.String("field", "netbird."+f.key),
				)
			}
		}
	}

	if wantManagedKeycloak {
		keycloak, err := ensureMappingChild(docMap, "keycloak")
		if err != nil {
			return err
		}
		// adminPassword: required for managed Keycloak.
		// netBirdBackendClientSecret: in managed mode kubeaid-cli
		// creates the netbird-backend OIDC client itself; persisting
		// the secret here means the realm reconciler and the netbird
		// SealedSecret read the same source-of-truth value, so they
		// can never drift.
		fields := []string{"adminPassword", "netBirdBackendClientSecret"}
		for _, f := range fields {
			wrote, err := setScalarIfEmpty(keycloak, f, randval.Password)
			if err != nil {
				return err
			}
			if wrote {
				changed = true
				slog.InfoContext(ctx, "Generated and persisted to secrets.yaml",
					slog.String("field", "keycloak."+f),
				)
			}
		}
	}

	if !changed {
		return nil
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshalling secrets.yaml: %w", err)
	}
	if err := renameio.WriteFile(secretsPath, out, 0o600); err != nil {
		return fmt.Errorf("writing secrets.yaml: %w", err)
	}

	// Refresh ParsedSecretsConfig from the mutated YAML so every
	// caller from here on sees the just-generated values without
	// having to re-parse downstream.
	config.ParsedSecretsConfig = &config.SecretsConfig{}
	if err := yaml.Unmarshal(out, config.ParsedSecretsConfig); err != nil {
		return fmt.Errorf("re-unmarshalling secrets.yaml after fill: %w", err)
	}
	return nil
}

// documentRootMapping returns the mapping node at the root of the
// YAML document — i.e. node.Content[0] when the document is a
// single mapping. yaml.Unmarshal on a normal config file produces
// Kind=DocumentNode with one child mapping; this helper unwraps
// that consistently and returns a clear error if the document is
// shaped unexpectedly.
func documentRootMapping(node *yaml.Node) (*yaml.Node, error) {
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		// Empty file or unexpected shape — synthesize a fresh root
		// mapping so the caller can append into it.
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		node.Kind = yaml.DocumentNode
		node.Content = []*yaml.Node{mapping}
		return mapping, nil
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("secrets.yaml root must be a mapping, got %v", root.Kind)
	}
	return root, nil
}

// ensureMappingChild returns the mapping value for the given key
// under parent. If the key is absent, missing, null, or has a
// scalar zero value, it's (re)created as an empty mapping node.
// Lets callers descend into a guaranteed-mapping subtree to add
// fields without worrying about whether the operator wrote
// `netbird:` (null) vs `netbird: {}` vs nothing at all.
func ensureMappingChild(parent *yaml.Node, key string) (*yaml.Node, error) {
	if parent.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parent node is not a mapping (kind=%v)", parent.Kind)
	}

	for i := 0; i+1 < len(parent.Content); i += 2 {
		k := parent.Content[i]
		v := parent.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Value != key {
			continue
		}
		if v.Kind != yaml.MappingNode {
			// Operator wrote `netbird:` or `netbird: null` — turn the
			// value into an empty mapping in place so we can append
			// children. Preserves existing comments on the key node.
			*v = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		return v, nil
	}

	// Key not present — append a fresh key + empty mapping pair.
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyNode, valNode)
	return valNode, nil
}

// setScalarIfEmpty fills mapping[key] with a freshly-generated
// value only when the existing value is missing or an empty string.
// Returns true if a write happened. gen is invoked lazily so we
// don't burn entropy generating values that won't be used.
func setScalarIfEmpty(mapping *yaml.Node, key string, gen func() (string, error)) (bool, error) {
	if mapping.Kind != yaml.MappingNode {
		return false, fmt.Errorf("expected mapping for key=%s, got kind=%v", key, mapping.Kind)
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Value != key {
			continue
		}
		// Existing key. Only generate if value is empty / null.
		if v.Kind == yaml.ScalarNode && v.Value != "" && v.Tag != "!!null" {
			return false, nil
		}
		value, err := gen()
		if err != nil {
			return false, err
		}
		v.Kind = yaml.ScalarNode
		v.Tag = "!!str"
		v.Value = value
		v.Style = 0
		return true, nil
	}

	// Key not present — append.
	value, err := gen()
	if err != nil {
		return false, err
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	mapping.Content = append(mapping.Content, keyNode, valNode)
	return true, nil
}
