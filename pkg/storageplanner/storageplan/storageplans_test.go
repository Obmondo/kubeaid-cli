// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAreStoragePlansAlike(t *testing.T) {
	mkPlan := func(zfsDiskNames ...string) *StoragePlan {
		disks := make([]*Disk, len(zfsDiskNames))
		for i, name := range zfsDiskNames {
			disks[i] = &Disk{Name: name}
		}
		return &StoragePlan{ZFS: disks}
	}

	tests := []struct {
		name  string
		plans []*StoragePlan
		want  bool
	}{
		{
			name:  "empty input",
			plans: nil,
			want:  true,
		},
		{
			name:  "single plan",
			plans: []*StoragePlan{mkPlan("nvme0n1", "nvme1n1")},
			want:  true,
		},
		{
			name: "all plans share the same ZFS disks",
			plans: []*StoragePlan{
				mkPlan("nvme0n1", "nvme1n1"),
				mkPlan("nvme0n1", "nvme1n1"),
				mkPlan("nvme0n1", "nvme1n1"),
			},
			want: true,
		},
		{
			name: "first disk diverges",
			plans: []*StoragePlan{
				mkPlan("nvme0n1", "nvme1n1"),
				mkPlan("sda", "sdb"),
			},
			want: false,
		},
		{
			name: "second disk diverges",
			plans: []*StoragePlan{
				mkPlan("sda", "sdb"),
				mkPlan("sda", "sdc"),
			},
			want: false,
		},
		{
			name: "later plan has more ZFS disks than the first",
			plans: []*StoragePlan{
				mkPlan("nvme0n1"),
				mkPlan("nvme0n1", "nvme1n1"),
			},
			want: false,
		},
		{
			name: "later plan has fewer ZFS disks than the first",
			plans: []*StoragePlan{
				mkPlan("nvme0n1", "nvme1n1"),
				mkPlan("nvme0n1"),
			},
			want: false,
		},
		{
			name: "all plans have no ZFS disks",
			plans: []*StoragePlan{
				mkPlan(),
				mkPlan(),
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, AreStoragePlansAlike(tc.plans))
		})
	}
}
