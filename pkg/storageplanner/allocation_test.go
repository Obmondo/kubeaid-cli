// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/storageplanner/storageplan"
)

func testDisk(name, diskType string, sizeGB int) *storageplan.Disk {
	d := &storageplan.Disk{Name: name, Type: diskType, Size: sizeGB}
	d.AssignPriorityScores()
	return d
}

func TestAllocateStoragePlan(t *testing.T) {
	tests := []struct {
		name       string
		serverID   string
		disks      func() []*storageplan.Disk
		osDiskSize int
		minZFSSize int
		wantErr    bool
		wantErrSub string
		assertPlan func(t *testing.T, plan *storageplan.StoragePlan, disks []*storageplan.Disk)
	}{
		{
			name:     "selects OS by priority then name",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("sdb", constants.DiskTypeHDD, 500),
					testDisk("sda", constants.DiskTypeHDD, 500),
					testDisk("nvme0n1", constants.DiskTypeNVMe, 500),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			assertPlan: func(t *testing.T, plan *storageplan.StoragePlan, _ []*storageplan.Disk) {
				assert.Equal(t, []string{"sda", "sdb"}, diskNames(plan.OS))
			},
		},
		{
			name:     "selects ZFS by priority then name",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("nvme1n1", constants.DiskTypeNVMe, 500),
					testDisk("sda", constants.DiskTypeHDD, 500),
					testDisk("nvme0n1", constants.DiskTypeNVMe, 500),
					testDisk("sdb", constants.DiskTypeHDD, 500),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			assertPlan: func(t *testing.T, plan *storageplan.StoragePlan, _ []*storageplan.Disk) {
				assert.Equal(t, []string{"nvme0n1", "nvme1n1"}, diskNames(plan.ZFS))
			},
		},
		{
			name:     "returns OS error when less than two disks fit",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("sda", constants.DiskTypeHDD, 500),
					testDisk("sdb", constants.DiskTypeHDD, 49),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			wantErr:    true,
			wantErrSub: "OS",
		},
		{
			name:     "returns ZFS error when less than two disks fit after OS",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("sda", constants.DiskTypeHDD, 120),
					testDisk("sdb", constants.DiskTypeHDD, 120),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			wantErr:    true,
			wantErrSub: "ZFS",
		},
		{
			name:     "allocates CEPH from remaining space",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("sda", constants.DiskTypeHDD, 300),
					testDisk("sdb", constants.DiskTypeHDD, 300),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			assertPlan: func(t *testing.T, plan *storageplan.StoragePlan, _ []*storageplan.Disk) {
				require.Len(t, plan.CEPH, 2)
				assert.Equal(t, 150, plan.CEPH[0].Allocations.CEPH)
				assert.Equal(t, 150, plan.CEPH[1].Allocations.CEPH)
			},
		},
		{
			name:     "skips CEPH when remaining space below minimum",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("sda", constants.DiskTypeHDD, 190),
					testDisk("sdb", constants.DiskTypeHDD, 190),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			assertPlan: func(t *testing.T, plan *storageplan.StoragePlan, _ []*storageplan.Disk) {
				assert.Empty(t, plan.CEPH)
			},
		},
		{
			name:     "keeps server ID and disk list",
			serverID: "srv-123",
			disks: func() []*storageplan.Disk {
				return []*storageplan.Disk{
					testDisk("sda", constants.DiskTypeHDD, 500),
					testDisk("sdb", constants.DiskTypeHDD, 500),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			assertPlan: func(t *testing.T, plan *storageplan.StoragePlan, disks []*storageplan.Disk) {
				assert.Equal(t, "srv-123", plan.ServerID)
				assert.Same(t, disks[0], plan.Disks[0])
				assert.Same(t, disks[1], plan.Disks[1])
			},
		},
		{
			name:     "uses existing allocations when checking capacity",
			serverID: "srv",
			disks: func() []*storageplan.Disk {
				sda := testDisk("sda", constants.DiskTypeHDD, 500)
				sda.Allocations.CEPH = 460

				return []*storageplan.Disk{
					sda,
					testDisk("sdb", constants.DiskTypeHDD, 500),
					testDisk("sdc", constants.DiskTypeHDD, 500),
				}
			},
			osDiskSize: 50,
			minZFSSize: 100,
			assertPlan: func(t *testing.T, plan *storageplan.StoragePlan, _ []*storageplan.Disk) {
				assert.Equal(t, []string{"sdb", "sdc"}, diskNames(plan.OS))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disks := tc.disks()

			plan, err := allocateStoragePlan(tc.serverID, disks, tc.osDiskSize, tc.minZFSSize)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, plan)

			if tc.assertPlan != nil {
				tc.assertPlan(t, plan, disks)
			}
		})
	}
}

func diskNames(disks []*storageplan.Disk) []string {
	names := make([]string, len(disks))
	for i, disk := range disks {
		names[i] = disk.Name
	}
	return names
}
