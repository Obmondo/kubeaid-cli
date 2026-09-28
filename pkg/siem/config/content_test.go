// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const contentBase = `{
	"domain": "example.com",
	"keycloak": {"url": "http://kc", "realm": "r", "adminSecretRef": {"namespace": "n", "name": "s", "key": "k"}},
	"operators": {},
	"tenants": [{"code": "a1", "name": "A"}],
	"components": {
		"wazuh": [{"tenant": "a1", "url": "https://w", "credSecretRef": {"namespace": "n", "name": "c"}}],
		"content": %s
	}
}`

func TestContentDefaultsAndValidation(t *testing.T) {
	t.Parallel()
	parse := func(content string) (*Config, error) {
		return Parse([]byte(fmt.Sprintf(contentBase, content)))
	}
	cfg, err := parse(`{"dir": "/etc/kubesoc-content", "stateNamespace": "security-operations", "canary": "a1"}`)
	require.NoError(t, err)
	assert.Equal(t, DefaultContentStatePrefix, cfg.Components.Content.StatePrefix)

	_, err = parse(`{"velociraptor": true, "canary": "zz"}`)
	require.Error(t, err)
	for _, want := range []string{"content.dir is required", "stateNamespace is required", "needs components.velociraptor", `canary "zz"`} {
		assert.ErrorContains(t, err, want)
	}
}
