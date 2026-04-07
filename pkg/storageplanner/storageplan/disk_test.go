// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func TestAllocated(t *testing.T) {
	d := Disk{
		Size:        500,
		Allocations: struct{ OS, ZFS, CEPH int }{OS: 50, ZFS: 100, CEPH: 200},
	}
	assert.Equal(t, 350, d.Allocated())

	empty := Disk{Size: 500}
	assert.Equal(t, 0, empty.Allocated())
}

func TestUnallocated(t *testing.T) {
	d := Disk{
		Size:        500,
		Allocations: struct{ OS, ZFS, CEPH int }{OS: 50, ZFS: 100},
	}
	assert.Equal(t, 350, d.Unallocated())

	full := Disk{
		Size:        500,
		Allocations: struct{ OS, ZFS, CEPH int }{OS: 50, ZFS: 100, CEPH: 350},
	}
	assert.Equal(t, 0, full.Unallocated())
}

func TestAssignPriorityScores(t *testing.T) {
	for _, tc := range []struct {
		diskType         string
		withHighSpeedNIC bool
		os, zfs          int
	}{
		{constants.DiskTypeHDD, false, 3, 2},
		{constants.DiskTypeSSD, false, 2, 3},
		{constants.DiskTypeNVMe, false, 1, 4},
		{constants.DiskTypeUnknown, false, 0, 0},

		{constants.DiskTypeHDD, true, 3, 2},
		{constants.DiskTypeSSD, true, 2, 5},
		{constants.DiskTypeNVMe, true, 1, 6},
	} {
		d := &Disk{Type: tc.diskType, WithHighSpeedNIC: tc.withHighSpeedNIC}
		d.AssignPriorityScores()
		assert.Equal(t, tc.os, d.PriorityScores.OS,
			"OS score for %s (highNIC=%v)", tc.diskType, tc.withHighSpeedNIC)
		assert.Equal(t, tc.zfs, d.PriorityScores.ZFS,
			"ZFS score for %s (highNIC=%v)", tc.diskType, tc.withHighSpeedNIC)
	}
}

func TestAssignPriorityScoresOverwritesPrevious(t *testing.T) {
	d := &Disk{
		Type:           constants.DiskTypeHDD,
		PriorityScores: PriorityScores{OS: 99, ZFS: 99},
	}
	d.AssignPriorityScores()
	assert.Equal(t, 3, d.PriorityScores.OS)
	assert.Equal(t, 2, d.PriorityScores.ZFS)
}
