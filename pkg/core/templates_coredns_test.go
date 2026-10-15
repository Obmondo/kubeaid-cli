// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const corednsTemplatePath = "templates/k8s-configs/coredns.configmap.yaml.tmpl"

// TestCorednsHostsBlockPrivateIPOnly pins the hosts block to a single A
// record for the apiserver endpoint, even when the LB still carries its
// bootstrap public IP. The Corefile ends in `loadbalance`, so a second
// record for the same name would round-robin half of all lookups onto an
// address that gets disabled once the cluster is up.
func TestCorednsHostsBlockPrivateIPOnly(t *testing.T) {
	out := renderEmbeddedTemplate(t, corednsTemplatePath, TemplateValues{
		ControlPlaneEndpoint:            "api.demo.example.com",
		ControlPlaneLBPrivateIP:         "10.0.0.3",
		ControlPlaneLBBootstrapPublicIP: "203.0.113.10",
	})

	// Parse the output so an indentation slip inside the Corefile literal
	// block surfaces here rather than as a CoreDNS crashloop.
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &parsed),
		"rendered coredns ConfigMap must be valid YAML:\n%s", out)

	assert.Contains(t, out, "            10.0.0.3 api.demo.example.com",
		"the LB private IP must resolve the apiserver endpoint for pods")
	assert.NotContains(t, out, "203.0.113.10",
		"the LB bootstrap public IP must never reach the hosts block")
	assert.Equal(t, 1, strings.Count(out, "api.demo.example.com"),
		"exactly one A record, else loadbalance round-robins onto a dead IP")
}
