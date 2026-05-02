// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoragePlanGetUITree(t *testing.T) {
	tests := []struct {
		name           string
		plan           *StoragePlan
		wantSubstrings []string
		notWant        []string
	}{
		{
			name: "fully allocated disk hides the unallocated hint",
			plan: &StoragePlan{
				ServerID: "server-01",
				Disks: []*Disk{
					func() *Disk {
						d := &Disk{Name: "nvme0n1", Size: 500}
						d.Allocations.OS = 200
						d.Allocations.ZFS = 200
						d.Allocations.CEPH = 100
						return d
					}(),
				},
			},
			wantSubstrings: []string{
				"server-01",
				"nvme0n1",
				"OS   : 200 GB",
				"ZFS  : 200 GB",
				"CEPH : 100 GB",
			},
			notWant: []string{"unallocated"},
		},
		{
			name: "partially allocated disk shows unallocated remainder",
			plan: &StoragePlan{
				ServerID: "server-02",
				Disks: []*Disk{
					func() *Disk {
						d := &Disk{Name: "sda", Size: 1000}
						d.Allocations.OS = 100
						return d
					}(),
				},
			},
			wantSubstrings: []string{
				"server-02",
				"sda (900 GB unallocated)",
				"OS   : 100 GB",
			},
			notWant: []string{"ZFS", "CEPH"},
		},
		{
			name: "untouched disk shows full size as unallocated",
			plan: &StoragePlan{
				ServerID: "server-03",
				Disks: []*Disk{
					{Name: "sdb", Size: 250},
				},
			},
			wantSubstrings: []string{
				"server-03",
				"sdb (250 GB unallocated)",
			},
			notWant: []string{"OS", "ZFS", "CEPH"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rendered := tc.plan.getUITree().String()
			require.NotEmpty(t, rendered)

			for _, want := range tc.wantSubstrings {
				assert.True(
					t,
					strings.Contains(rendered, want),
					"expected rendered tree to contain %q\n--- rendered ---\n%s",
					want, rendered,
				)
			}
			for _, notWant := range tc.notWant {
				assert.False(
					t,
					strings.Contains(rendered, notWant),
					"expected rendered tree to NOT contain %q\n--- rendered ---\n%s",
					notWant, rendered,
				)
			}
		})
	}
}
