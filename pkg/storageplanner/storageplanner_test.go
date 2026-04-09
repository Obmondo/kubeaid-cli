// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplanner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

type mockExecutor struct {
	// commands is a list of outputs returned in order.
	commands []string
	callIdx  int
}

func (m *mockExecutor) Execute(_ context.Context, _ string) (string, error) {
	out := m.commands[m.callIdx]
	m.callIdx++
	return out, nil
}

func (m *mockExecutor) MustExecute(ctx context.Context, cmd string) string {
	out, _ := m.Execute(ctx, cmd)
	return out
}

// newMock creates a mock that returns nicSpeed for the first call (NIC speed query)
// and lsblk JSON for the second call.
func newMock(nicSpeed string, devices []LSBLKOutputRow) *mockExecutor {
	lsblk := LSBLKOutput{BlockDevices: devices}
	data, _ := json.Marshal(lsblk)
	return &mockExecutor{commands: []string{nicSpeed, string(data)}}
}

func device(name, tran string, sizeGB int, rota bool) LSBLKOutputRow {
	return LSBLKOutputRow{
		Name:               name,
		WWN:                "0x" + name,
		Size:               (sizeGB + 2) * 1024 * 1024 * 1024, // +2GB for boot/EFI reserve
		RotationalDevice:   rota,
		TransportType:      tran,
		PartitionTableType: "gpt",
	}
}

func hdd(name string, sizeGB int) LSBLKOutputRow { return device(name, "sata", sizeGB, true) }

func ssd(name string, sizeGB int) LSBLKOutputRow { return device(name, "sata", sizeGB, false) }

func nvme(name string, sizeGB int) LSBLKOutputRow { return device(name, "nvme", sizeGB, false) }

func TestGetDiskType(t *testing.T) {
	assert.Equal(t, constants.DiskTypeHDD,
		(&LSBLKOutputRow{RotationalDevice: true, TransportType: "sata"}).GetDiskType())
	assert.Equal(t, constants.DiskTypeSSD,
		(&LSBLKOutputRow{RotationalDevice: false, TransportType: "sata"}).GetDiskType())
	assert.Equal(t, constants.DiskTypeNVMe,
		(&LSBLKOutputRow{RotationalDevice: false, TransportType: "nvme"}).GetDiskType())

	// rotational bit takes precedence over transport type
	assert.Equal(t, constants.DiskTypeHDD,
		(&LSBLKOutputRow{RotationalDevice: true, TransportType: "nvme"}).GetDiskType())

	assert.Equal(t, constants.DiskTypeUnknown,
		(&LSBLKOutputRow{RotationalDevice: false, TransportType: "usb"}).GetDiskType())
}

func TestGenerateStoragePlan_FourIdenticalHDDs(t *testing.T) {
	mock := newMock("1000\n", []LSBLKOutputRow{
		hdd("sda", 500), hdd("sdb", 500), hdd("sdc", 500), hdd("sdd", 500),
	})

	plan, err := GenerateStoragePlan(context.Background(), "srv1", mock, 50, 100)
	require.NoError(t, err)

	assert.Equal(t, "srv1", plan.ServerID)
	require.Len(t, plan.OS, 2)
	require.Len(t, plan.ZFS, 2)

	// identical priority -> alphabetical selection
	assert.Equal(t, "sda", plan.OS[0].Name)
	assert.Equal(t, "sdb", plan.OS[1].Name)
	assert.Equal(t, 50, plan.OS[0].Allocations.OS)

	assert.Equal(t, "sda", plan.ZFS[0].Name)
	assert.Equal(t, 100, plan.ZFS[0].Allocations.ZFS)

	// all 4 disks have >=50GB remaining -> all get CEPH
	assert.Len(t, plan.CEPH, 4)
}

func TestGenerateStoragePlan_HDDsForOS_NVMeForZFS(t *testing.T) {
	mock := newMock("1000\n", []LSBLKOutputRow{
		nvme("nvme0n1", 500), nvme("nvme1n1", 500),
		hdd("sda", 500), hdd("sdb", 500),
	})

	plan, err := GenerateStoragePlan(context.Background(), "srv2", mock, 50, 100)
	require.NoError(t, err)

	// OS priority: HDD=3 > NVMe=1
	assert.Equal(t, "sda", plan.OS[0].Name)
	assert.Equal(t, "sdb", plan.OS[1].Name)

	// ZFS priority: NVMe=5 > HDD=3
	assert.Equal(t, "nvme0n1", plan.ZFS[0].Name)
	assert.Equal(t, "nvme1n1", plan.ZFS[1].Name)
}

func TestGenerateStoragePlan_HighSpeedNIC_HDDsPreferredForZFS(t *testing.T) {
	// With a high-speed NIC (>= 5000 Mbps), SSDs get a low ZFS score (2) so that
	// CEPH ends up on SSDs and can exploit the higher network bandwidth.
	// HDDs therefore win the ZFS selection (score 3 > 2).
	mock := newMock("10000\n", []LSBLKOutputRow{
		ssd("sda", 500), ssd("sdb", 500),
		hdd("sdc", 500), hdd("sdd", 500),
	})

	plan, err := GenerateStoragePlan(context.Background(), "srv8", mock, 50, 100)
	require.NoError(t, err)

	// OS priority: HDD=3 > SSD=2
	assert.Equal(t, "sdc", plan.OS[0].Name)
	assert.Equal(t, "sdd", plan.OS[1].Name)

	// ZFS priority: HDD=3 > SSD with high-speed NIC=2 (SSDs reserved for CEPH)
	assert.Equal(t, "sdc", plan.ZFS[0].Name)
	assert.Equal(t, "sdd", plan.ZFS[1].Name)
}

func TestGenerateStoragePlan_NotEnoughDisksForOS(t *testing.T) {
	mock := newMock("1000\n", []LSBLKOutputRow{hdd("sda", 500)})

	_, err := GenerateStoragePlan(context.Background(), "srv3", mock, 50, 100)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "OS"))
}

func TestGenerateStoragePlan_NotEnoughSpaceForZFS(t *testing.T) {
	// OS eats 80GB, leaving 20GB per disk — not enough for 100GB ZFS.
	mock := newMock("1000\n", []LSBLKOutputRow{hdd("sda", 100), hdd("sdb", 100)})

	_, err := GenerateStoragePlan(context.Background(), "srv4", mock, 80, 100)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "ZFS"))
}

func TestGenerateStoragePlan_NoCEPHWhenNoSpaceLeft(t *testing.T) {
	// 200GB disks, OS=80, ZFS=80 -> 40GB remaining < 50GB CEPH minimum.
	mock := newMock("1000\n", []LSBLKOutputRow{hdd("sda", 200), hdd("sdb", 200)})

	plan, err := GenerateStoragePlan(context.Background(), "srv5", mock, 80, 80)
	require.NoError(t, err)
	assert.Empty(t, plan.CEPH)
}

func TestGenerateStoragePlan_SmallDisksSkipped(t *testing.T) {
	// sda is big enough, sdb and sdc are too small for OS=50.
	mock := newMock("1000\n", []LSBLKOutputRow{
		hdd("sda", 500), hdd("sdb", 10), hdd("sdc", 10),
	})

	_, err := GenerateStoragePlan(context.Background(), "srv6", mock, 50, 100)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "OS"))
}

func TestGenerateStoragePlan_UnknownDiskTypeFiltered(t *testing.T) {
	mock := newMock("1000\n", []LSBLKOutputRow{
		hdd("sda", 500),
		hdd("sdb", 500),
		device("sdc", "usb", 500, false), // usb -> unknown -> filtered
	})

	plan, err := GenerateStoragePlan(context.Background(), "srv7", mock, 50, 100)
	require.NoError(t, err)
	assert.Len(t, plan.Disks, 2)
}
