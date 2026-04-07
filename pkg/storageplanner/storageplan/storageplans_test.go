// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAreStoragePlansAlike(t *testing.T) {
	mkPlan := func(names ...string) *StoragePlan {
		disks := make([]*Disk, len(names))
		for i, n := range names {
			disks[i] = &Disk{Name: n}
		}
		return &StoragePlan{ZFS: disks}
	}

	assert.True(t, AreStoragePlansAlike([]*StoragePlan{
		mkPlan("nvme0n1", "nvme1n1"),
	}))

	assert.True(t, AreStoragePlansAlike([]*StoragePlan{
		mkPlan("nvme0n1", "nvme1n1"),
		mkPlan("nvme0n1", "nvme1n1"),
		mkPlan("nvme0n1", "nvme1n1"),
	}))

	assert.False(t, AreStoragePlansAlike([]*StoragePlan{
		mkPlan("nvme0n1", "nvme1n1"),
		mkPlan("sda", "sdb"),
	}))

	// first disk matches, second doesn't
	assert.False(t, AreStoragePlansAlike([]*StoragePlan{
		mkPlan("sda", "sdb"),
		mkPlan("sda", "sdc"),
	}))
}
