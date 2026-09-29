// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/randval"
)

// YAML core schema tags of the nodes the secrets fill reads and writes.
const (
	yamlTagStr  = "!!str"
	yamlTagMap  = "!!map"
	yamlTagNull = "!!null"
)

// wazuhPasswordHashCost is the bcrypt cost of the indexer and dashboard
// password hashes (the OpenSearch security plugin's internal_users.yml).
const wazuhPasswordHashCost = 12

// wazuhAPIPasswordPrefix makes a generated password meet the Wazuh API policy
// (8-64 characters with upper and lower case, a digit and a symbol) whatever
// the random part holds. The dot is shell and YAML safe.
const wazuhAPIPasswordPrefix = "Aa1."

// securityOperationsHashedPasswords are the password fields that come with a
// bcrypt hash: password key -> hash key.
var securityOperationsHashedPasswords = [][2]string{
	{"indexerPassword", "indexerPasswordHash"},
	{"dashboardPassword", "dashboardPasswordHash"},
}

// fillSecurityOperationsSecrets generates the Wazuh logins of the central
// search and of every tenant into secrets.yaml `securityOperations`, and keeps
// each bcrypt hash in step with its password: a hash is (re)generated when it
// is empty or does not match. Existing passwords are never changed, and
// entries of tenants no longer in general.yaml are left alone. Returns whether
// anything was written.
func fillSecurityOperationsSecrets(ctx context.Context,
	docMap *yaml.Node,
	tenants []config.SecurityOperationsTenant,
) (bool, error) {
	root, err := ensureMappingChild(docMap, "securityOperations")
	if err != nil {
		return false, err
	}

	central, err := ensureMappingChild(root, "central")
	if err != nil {
		return false, err
	}
	changed, err := fillWazuhCredentials(ctx, central, "securityOperations.central", false)
	if err != nil {
		return changed, err
	}

	if len(tenants) == 0 {
		return changed, nil
	}

	tenantsNode, err := ensureMappingChild(root, "tenants")
	if err != nil {
		return changed, err
	}
	for _, tenant := range tenants {
		tenantNode, err := ensureMappingChild(tenantsNode, tenant.Code)
		if err != nil {
			return changed, err
		}
		quoteMappingKey(tenantsNode, tenant.Code)

		wrote, err := fillWazuhCredentials(ctx, tenantNode,
			"securityOperations.tenants."+tenant.Code, true)
		if err != nil {
			return changed, err
		}
		changed = changed || wrote
	}

	return changed, nil
}

// fillWazuhCredentials fills one Wazuh release's logins in mapping. Tenant
// releases also get the manager API, authd and cluster key.
func fillWazuhCredentials(ctx context.Context, mapping *yaml.Node, path string, tenant bool) (bool, error) {
	type field struct {
		key string
		gen func() (string, error)
	}
	fields := []field{
		{"indexerPassword", randval.Password},
		{"dashboardPassword", randval.Password},
	}
	if tenant {
		fields = append(fields,
			field{"apiPassword", wazuhAPIPassword},
			field{"authdPassword", randval.Password},
			field{"clusterKey", randval.Password},
		)
	}

	changed := false
	logWrite := func(key string) {
		changed = true
		slog.InfoContext(ctx, "Generated and persisted to secrets.yaml",
			slog.String("field", path+"."+key),
		)
	}

	for _, f := range fields {
		wrote, err := setScalarIfEmpty(mapping, f.key, f.gen)
		if err != nil {
			return changed, err
		}
		if wrote {
			logWrite(f.key)
		}
	}

	for _, pair := range securityOperationsHashedPasswords {
		wrote, err := setBcryptHashIfStale(mapping, pair[0], pair[1])
		if err != nil {
			return changed, fmt.Errorf("hashing %s.%s: %w", path, pair[0], err)
		}
		if wrote {
			logWrite(pair[1])
		}
	}

	return changed, nil
}

// wazuhAPIPassword returns a random password the Wazuh API accepts.
func wazuhAPIPassword() (string, error) {
	random, err := randval.Password()
	if err != nil {
		return "", err
	}
	return wazuhAPIPasswordPrefix + random[:randval.PasswordLength-len(wazuhAPIPasswordPrefix)], nil
}

// setBcryptHashIfStale sets mapping[hashKey] to a bcrypt hash of
// mapping[passwordKey] unless the current hash already matches the password,
// so renders stay byte-stable across runs and follow a password change.
func setBcryptHashIfStale(mapping *yaml.Node, passwordKey, hashKey string) (bool, error) {
	password := scalarValue(mapping, passwordKey)
	if password == "" {
		return false, fmt.Errorf("%s is empty", passwordKey)
	}

	if current := scalarValue(mapping, hashKey); current != "" &&
		bcrypt.CompareHashAndPassword([]byte(current), []byte(password)) == nil {
		return false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), wazuhPasswordHashCost)
	if err != nil {
		return false, err
	}
	setScalar(mapping, hashKey, string(hash))
	return true, nil
}

// scalarValue returns mapping[key] when it is a non-null scalar, "" otherwise.
func scalarValue(mapping *yaml.Node, key string) string {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k, v := mapping.Content[i], mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			if v.Kind == yaml.ScalarNode && v.Tag != yamlTagNull {
				return v.Value
			}
			return ""
		}
	}
	return ""
}

// setScalar sets mapping[key] to the string value, adding the key if needed.
func setScalar(mapping *yaml.Node, key, value string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k, v := mapping.Content[i], mapping.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			*v = yaml.Node{Kind: yaml.ScalarNode, Tag: yamlTagStr, Value: value}
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlTagStr, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: yamlTagStr, Value: value},
	)
}

// quoteMappingKey double-quotes the key node, so a tenant code such as "001"
// stays a string when the file is read back by other YAML tools.
func quoteMappingKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if k := mapping.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			k.Tag = yamlTagStr
			k.Style = yaml.DoubleQuotedStyle
			return
		}
	}
}
