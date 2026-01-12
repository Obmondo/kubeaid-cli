// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"runtime"
	"testing"

	coreV1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/assert"
)

type (
	// Non secret configuration options.
	GeneralConfig struct {
		// KubeAid and KubeAid Config repository specific details.
		// The KubeAid and KubeAid Config repositories must be hosted in the same Git server.
		Forks ForksConfig `yaml:"forkURLs" validate:"required"`

		// Cloud provider specific details.
		Cloud CloudConfig `yaml:"cloud" validate:"required"`
	}

	// KubeAid and KubeAid Config repository speicific details.
	// For now, we require the KubeAid and KubeAid Config repositories to be hosted in the same
	// Git server.
	ForksConfig struct {
		// KubeAid repository specific details.
		KubeaidFork KubeAidForkConfig `yaml:"kubeaid" validate:"required"`

		// KubeAid Config repository specific details.
		KubeaidConfigFork KubeaidConfigForkConfig `yaml:"kubeaidConfig" validate:"required"`
	}

	// KubeAid repository specific details.
	KubeAidForkConfig struct {
		// KubeAid repository (HTTPS) URL.
		URL string `yaml:"url" default:"https://github.com/Obmondo/KubeAid" validate:"notblank"`

		// KubeAid tag.
		Version string `yaml:"version" validate:"notblank"`
	}

	// KubeAid Config repository specific details.
	KubeaidConfigForkConfig struct {
		// KubeAid repository (HTTPS) URL.
		URL string `yaml:"url" validate:"notblank"`

		// Name of the directory inside your KubeAid Config repository's k8s folder, where the KubeAid
		// Config files for this cluster will be contained.
		//
		// When not specified, the directory name will default to the cluster name.
		//
		// So, suppose your cluster name is 'staging'. Then, the directory name will default to
		// 'staging'. Or you can customize it to something like 'staging.qa'.
		Directory string `yaml:"directory"`
	}

	// Cloud provider specific details.
	// Make sure you fillup details specific to only 1 cloud provider. Otherwise, the first cloud
	// provider with non-empty details will be picked up.
	CloudConfig struct {
		// Hetzner specific details.
		Hetzner *HetznerConfig `yaml:"hetzner"`
	}

	// Hetzner specific details.
	HetznerConfig struct {
		Mode string `yaml:"mode" default:"hcloud" validate:"notblank,oneof=bare-metal hcloud hybrid"`

		// Details about node-groups in Hetzner.
		NodeGroups HetznerNodeGroups `yaml:"nodeGroups"`
	}

	// Details about node-groups in Hetzner.
	HetznerNodeGroups struct {
		// Details about node-groups in HCloud.
		HCloud []HCloudAutoScalableNodeGroup `yaml:"hcloud"`
	}

	// Details about (autoscalable) node-groups in HCloud.
	HCloudAutoScalableNodeGroup struct {
		AutoScalableNodeGroup `yaml:",inline"`

		// HCloud machine type.
		// You can browse all available HCloud machine types here : https://hetzner.com/cloud.
		MachineType string `yaml:"machineType" validate:"notblank"`

		// The root volume size for each HCloud machine.
		RootVolumeSize uint32 `validate:"required"`
	}

	NodeGroup struct {
		// Nodegroup name.
		Name string `yaml:"name" validate:"notblank"`

		// Labels that you want to be propagated to each node in the nodegroup.
		//
		// Each label should meet one of the following criterias to propagate to each of the nodes :
		//
		//   1. Has node-role.kubernetes.io as prefix.
		//   2. Belongs to node-restriction.kubernetes.io domain.
		//   3. Belongs to node.cluster.x-k8s.io domain.
		//
		// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
		Labels map[string]string `yaml:"labels"`

		// Taints that you want to be propagated to each node in the nodegroup.
		Taints []*coreV1.Taint `yaml:"taints"`
	}

	AutoScalableNodeGroup struct {
		NodeGroup `yaml:",inline"`

		CPU    uint32 `validate:"required"`
		Memory uint32 `validate:"required"`

		// Minimum number of replicas in the nodegroup.
		MinSize uint `yaml:"minSize" validate:"required"`

		// Maximum number of replicas in the nodegroup.
		Maxsize uint `yaml:"maxSize" validate:"required"`
	}
)

func TestGenerateConfigReference(t *testing.T) {
	_, thisFilePath, _, ok := runtime.Caller(0)
	assert.True(t, ok)

	_ = generateConfigReference(t.Context(), []string{thisFilePath})
}
