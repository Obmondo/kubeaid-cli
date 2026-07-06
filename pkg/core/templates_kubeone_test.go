// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/templates"
)

// TestKubeOneTemplateRendersKubeletConfig proves cloud.bare-metal.kubelet from general.yaml
// lands on every host entry (control-plane and workers) of the rendered KubeOne manifest,
// and stays absent when unset.
func TestKubeOneTemplateRendersKubeletConfig(t *testing.T) {
	controlPlaneAddress := "192.0.2.10"
	workerAddress := "192.0.2.20"
	maxPods := int32(300)

	values := &TemplateValues{
		ClusterConfig: config.ClusterConfig{Name: "demo", K8sVersion: "v1.35.6"},
		BareMetalConfig: &config.BareMetalConfig{
			SSH: config.BareMetalSSHConfig{Port: 22},
			Kubelet: &config.BareMetalKubeletConfig{
				MaxPods:        &maxPods,
				SystemReserved: map[string]string{"memory": "256Mi"},
			},
			ControlPlane: config.BareMetalControlPlane{
				Endpoint: config.BareMetalControlPlaneEndpoint{Host: controlPlaneAddress, Port: 6443},
				Hosts:    []*config.BareMetalHost{{PublicAddress: &controlPlaneAddress}},
			},
			NodeGroups: []config.BareMetalNodeGroup{{
				NodeGroup: config.NodeGroup{Name: "workers"},
				Hosts:     []*config.BareMetalHost{{PublicAddress: &workerAddress}},
			}},
		},
	}

	rendered := string(templates.ParseAndExecuteTemplate(
		t.Context(), &KubeaidConfigFileTemplates,
		"templates/kubeone/kubeone-cluster.yaml.tmpl", values,
	))
	t.Logf("--- rendered kubeone-cluster.yaml ---\n%s", rendered)

	// One kubelet block per host : 1 control-plane + 1 worker.
	assert.Equal(t, 2, strings.Count(rendered, "kubelet:"))
	assert.Equal(t, 2, strings.Count(rendered, "maxPods: 300"))
	assert.Equal(t, 2, strings.Count(rendered, "memory: 256Mi"))

	values.BareMetalConfig.Kubelet = nil
	rendered = string(templates.ParseAndExecuteTemplate(
		t.Context(), &KubeaidConfigFileTemplates,
		"templates/kubeone/kubeone-cluster.yaml.tmpl", values,
	))
	assert.NotContains(t, rendered, "kubelet:")
}
