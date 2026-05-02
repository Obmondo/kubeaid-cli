// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

func TestDiskAllocation(t *testing.T) {
	tests := []struct {
		name            string
		disk            *Disk
		allocOS         int
		allocZFS        int
		allocCEPH       int
		wantAllocated   int
		wantUnallocated int
	}{
		{
			name:            "no allocations on a 500 GB disk",
			disk:            NewDisk("", "", "", "", 500, false),
			wantAllocated:   0,
			wantUnallocated: 500,
		},
		{
			name:            "OS + ZFS + CEPH all set",
			disk:            NewDisk("", "", "", "", 500, false),
			allocOS:         50,
			allocZFS:        100,
			allocCEPH:       200,
			wantAllocated:   350,
			wantUnallocated: 150,
		},
		{
			name:            "fully allocated leaves zero unallocated",
			disk:            NewDisk("", "", "", "", 500, false),
			allocOS:         50,
			allocZFS:        100,
			allocCEPH:       350,
			wantAllocated:   500,
			wantUnallocated: 0,
		},
		{
			name:            "oversubscribed allocation gives negative unallocated",
			disk:            NewDisk("", "", "", "", 100, false),
			allocOS:         50,
			allocZFS:        80,
			wantAllocated:   130,
			wantUnallocated: -30,
		},
		{
			name:            "zero-size disk",
			disk:            NewDisk("", "", "", "", 0, false),
			wantAllocated:   0,
			wantUnallocated: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.disk.Allocations.OS = tc.allocOS
			tc.disk.Allocations.ZFS = tc.allocZFS
			tc.disk.Allocations.CEPH = tc.allocCEPH

			assert.Equal(t, tc.wantAllocated, tc.disk.Allocated())
			assert.Equal(t, tc.wantUnallocated, tc.disk.Unallocated())
		})
	}
}

func TestAssignPriorityScores(t *testing.T) {
	tests := []struct {
		name             string
		diskType         string
		withHighSpeedNIC bool
		preexisting      *PriorityScores
		wantOS           int
		wantZFS          int
	}{
		{name: "HDD without high-speed NIC", diskType: constants.DiskTypeHDD, wantOS: 3, wantZFS: 3},
		{name: "SSD without high-speed NIC", diskType: constants.DiskTypeSSD, wantOS: 2, wantZFS: 4},
		{name: "NVMe without high-speed NIC", diskType: constants.DiskTypeNVMe, wantOS: 1, wantZFS: 5},
		{name: "unknown disk type without NIC", diskType: constants.DiskTypeUnknown, wantOS: 0, wantZFS: 0},
		{
			name:             "HDD with high-speed NIC unaffected",
			diskType:         constants.DiskTypeHDD,
			withHighSpeedNIC: true,
			wantOS:           3,
			wantZFS:          3,
		},
		{
			name:             "SSD with high-speed NIC drops ZFS priority",
			diskType:         constants.DiskTypeSSD,
			withHighSpeedNIC: true,
			wantOS:           2,
			wantZFS:          2,
		},
		{
			name:             "NVMe with high-speed NIC drops ZFS priority",
			diskType:         constants.DiskTypeNVMe,
			withHighSpeedNIC: true,
			wantOS:           1,
			wantZFS:          1,
		},
		{
			name:             "unknown disk type with NIC stays 0",
			diskType:         constants.DiskTypeUnknown,
			withHighSpeedNIC: true,
			wantOS:           0,
			wantZFS:          0,
		},
		{name: "empty disk type defaults to 0", diskType: "", wantOS: 0, wantZFS: 0},
		{
			name:        "preexisting scores are overwritten",
			diskType:    constants.DiskTypeHDD,
			preexisting: &PriorityScores{OS: 99, ZFS: 99},
			wantOS:      3,
			wantZFS:     3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDisk("", "", tc.diskType, "", 0, tc.withHighSpeedNIC)
			if tc.preexisting != nil {
				d.PriorityScores = *tc.preexisting
			}
			d.AssignPriorityScores()
			assert.Equal(t, tc.wantOS, d.PriorityScores.OS, "OS score")
			assert.Equal(t, tc.wantZFS, d.PriorityScores.ZFS, "ZFS score")
		})
	}
}
