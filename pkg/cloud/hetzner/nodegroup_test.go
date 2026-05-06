// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner/storageplan"
)

func TestHydrateNodeGroupLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      *config.HetznerConfig
		wantLabels []map[string]string
	}{
		{
			name: "node group with NVMe ZFS disks gets disk=nvme label",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: []*config.HetznerBareMetalNodeGroup{
						{
							NodeGroup: config.NodeGroup{
								Name:   "workers",
								Labels: map[string]string{"existing": "value"},
							},
							StoragePlan: storageplan.StoragePlan{
								ZFS: []*storageplan.Disk{
									{Type: constants.DiskTypeNVMe},
								},
							},
						},
					},
				},
			},
			wantLabels: []map[string]string{
				{"existing": "value", "disk": "nvme"},
			},
		},
		{
			name: "node group without ZFS disks is not modified",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: []*config.HetznerBareMetalNodeGroup{
						{
							NodeGroup: config.NodeGroup{
								Name:   "workers",
								Labels: map[string]string{"existing": "value"},
							},
							StoragePlan: storageplan.StoragePlan{
								ZFS: []*storageplan.Disk{},
							},
						},
					},
				},
			},
			wantLabels: []map[string]string{
				{"existing": "value"},
			},
		},
		{
			name: "node group with non-NVMe ZFS disk is not modified",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: []*config.HetznerBareMetalNodeGroup{
						{
							NodeGroup: config.NodeGroup{
								Name:   "workers",
								Labels: map[string]string{"existing": "value"},
							},
							StoragePlan: storageplan.StoragePlan{
								ZFS: []*storageplan.Disk{
									{Type: constants.DiskTypeSSD},
								},
							},
						},
					},
				},
			},
			wantLabels: []map[string]string{
				{"existing": "value"},
			},
		},
		{
			name: "node group with nil Labels map gets map created",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: []*config.HetznerBareMetalNodeGroup{
						{
							NodeGroup: config.NodeGroup{
								Name: "workers",
							},
							StoragePlan: storageplan.StoragePlan{
								ZFS: []*storageplan.Disk{
									{Type: constants.DiskTypeNVMe},
								},
							},
						},
					},
				},
			},
			wantLabels: []map[string]string{
				{"disk": "nvme"},
			},
		},
		{
			name: "multiple node groups with mixed storage",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: []*config.HetznerBareMetalNodeGroup{
						{
							NodeGroup: config.NodeGroup{
								Name:   "nvme-workers",
								Labels: map[string]string{"role": "compute"},
							},
							StoragePlan: storageplan.StoragePlan{
								ZFS: []*storageplan.Disk{
									{Type: constants.DiskTypeNVMe},
								},
							},
						},
						{
							NodeGroup: config.NodeGroup{
								Name:   "hdd-workers",
								Labels: map[string]string{"role": "storage"},
							},
							StoragePlan: storageplan.StoragePlan{
								ZFS: []*storageplan.Disk{
									{Type: constants.DiskTypeHDD},
								},
							},
						},
					},
				},
			},
			wantLabels: []map[string]string{
				{"role": "compute", "disk": "nvme"},
				{"role": "storage"},
			},
		},
		{
			name: "empty bare metal node groups slice",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: []*config.HetznerBareMetalNodeGroup{},
				},
			},
			wantLabels: []map[string]string{},
		},
		{
			name: "nil bare metal node groups slice",
			input: &config.HetznerConfig{
				NodeGroups: config.HetznerNodeGroups{
					BareMetal: nil,
				},
			},
			wantLabels: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			hydrateNodeGroupLabels(tc.input)

			if tc.wantLabels == nil {
				assert.Nil(t, tc.input.NodeGroups.BareMetal)
				return
			}
			for i, ng := range tc.input.NodeGroups.BareMetal {
				assert.Equal(t, tc.wantLabels[i], ng.Labels)
			}
		})
	}
}
