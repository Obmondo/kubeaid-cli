// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestServerIDsHeader(t *testing.T) {
	mkPlans := func(ids ...string) []*StoragePlan {
		out := make([]*StoragePlan, len(ids))
		for i, id := range ids {
			out[i] = &StoragePlan{ServerID: id}
		}
		return out
	}

	tests := []struct {
		name  string
		plans []*StoragePlan
		want  string
	}{
		{
			name:  "single server uses singular phrasing",
			plans: mkPlans("100"),
			want:  "(server 100)",
		},
		{
			name:  "two servers list both",
			plans: mkPlans("100", "101"),
			want:  "(2 servers: 100, 101)",
		},
		{
			name:  "three servers (typical HA CP) list all",
			plans: mkPlans("100", "101", "102"),
			want:  "(3 servers: 100, 101, 102)",
		},
		{
			name:  "four servers list all",
			plans: mkPlans("100", "101", "102", "103"),
			want:  "(4 servers: 100, 101, 102, 103)",
		},
		{
			name:  "five servers truncate after four with a +N more suffix",
			plans: mkPlans("100", "101", "102", "103", "104"),
			want:  "(5 servers: 100, 101, 102, 103, …+1 more)",
		},
		{
			name:  "large node-group truncates aggressively",
			plans: mkPlans("100", "101", "102", "103", "104", "105", "106", "107"),
			want:  "(8 servers: 100, 101, 102, 103, …+4 more)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, serverIDsHeader(tc.plans))
		})
	}
}

// TestStoragePlansPrettyPrintRendersCompactSummary proves the
// per-group rendering uses the new compact shape (disk composition +
// ZFS pool sub-volume breakdown) rather than the old per-disk
// allocation subtree.
func TestStoragePlansPrettyPrintRendersCompactSummary(t *testing.T) {
	mkPlan := func(serverID, diskType string, size int) *StoragePlan {
		// Two disks per server, both of the same type+size — typical
		// 2 × NVMe AX52 shape. Mirror semantics make the visible pool
		// capacity equal to one disk's ZFS allocation.
		mk := func(name string) *Disk {
			d := &Disk{Name: name, Type: diskType, Size: size}
			d.Allocations.OS = 50
			d.Allocations.ZFS = 220
			return d
		}
		return &StoragePlan{
			ServerID: serverID,
			Disks:    []*Disk{mk("nvme0n1"), mk("nvme1n1")},
		}
	}

	plans := StoragePlans{
		"control-plane": {
			mkPlan("100", "NVMe", 1000),
			mkPlan("101", "NVMe", 1000),
			mkPlan("102", "NVMe", 1000),
		},
		"workers": {mkPlan("200", "HDD", 2000)},
	}

	got := captureStdout(t, plans.PrettyPrint)

	// Group headers (unchanged from the previous shape).
	assert.Contains(t, got, "control-plane (3 servers: 100, 101, 102)")
	assert.Contains(t, got, "workers (server 200)")

	// New shape — disk composition lines, one per group.
	assert.Contains(t, got, "Disks per server: 2 × NVMe 1 TB")
	assert.Contains(t, got, "Disks per server: 2 × HDD 2 TB")

	// ZFS pool header — names the pool, the RAID mode, and the OS-side facts.
	assert.Contains(t, got, `ZFS pool "primary" — mirror across 2 NVMe (OS: ext4 on RAID-1)`)
	assert.Contains(t, got, `ZFS pool "primary" — mirror across 2 HDD (OS: ext4 on RAID-1)`)

	// Sub-volume breakdown — rendered once per group.
	assert.Equal(t, 2, strings.Count(got, "/var/lib/containerd"))
	assert.Equal(t, 2, strings.Count(got, "100 GB"))
	assert.Equal(t, 2, strings.Count(got, "/var/log/pods"))
	assert.Equal(t, 2, strings.Count(got, "/var/lib/kubelet/pods"))
	assert.Equal(t, 2, strings.Count(got, "OpenEBS ZFS LocalPV"))
	// OpenEBS gets the pool's remainder above the three reserved sub-volumes
	// (220 - 100 - 50 - 50 = 20 GB on the default-sized pool).
	assert.Equal(t, 2, strings.Count(got, " 20 GB free"))

	// Old per-disk subtree must NOT render — that was the noisy shape.
	assert.NotContains(t, got, "nvme0n1 (730 GB unallocated)",
		"per-disk subtree should be gone — compact summary replaces it")
	assert.NotContains(t, got, "OS   : 50 GB",
		"per-disk allocation lines should be gone from the group rendering")
}

func TestFormatDiskComposition(t *testing.T) {
	mk := func(diskType string, size int) *Disk {
		return &Disk{Type: diskType, Size: size}
	}
	cases := []struct {
		name  string
		disks []*Disk
		want  string
	}{
		{
			name:  "two identical NVMe → collapsed with multiplicand",
			disks: []*Disk{mk("NVMe", 1000), mk("NVMe", 1000)},
			want:  "2 × NVMe 1 TB",
		},
		{
			name:  "single HDD → no multiplicand",
			disks: []*Disk{mk("HDD", 2000)},
			want:  "HDD 2 TB",
		},
		{
			name:  "heterogeneous → joined with ' + ', preserves first-seen order",
			disks: []*Disk{mk("NVMe", 1000), mk("HDD", 4000)},
			want:  "NVMe 1 TB + HDD 4 TB",
		},
		{
			name:  "mixed multiplicities → only the repeated type gets the count",
			disks: []*Disk{mk("NVMe", 1000), mk("NVMe", 1000), mk("HDD", 4000)},
			want:  "2 × NVMe 1 TB + HDD 4 TB",
		},
		{
			name:  "sub-TB stays in GB (operator-familiar Hetzner sizing)",
			disks: []*Disk{mk("NVMe", 100)},
			want:  "NVMe 100 GB",
		},
		{
			name:  "999 GB stays in GB (boundary)",
			disks: []*Disk{mk("NVMe", 999)},
			want:  "NVMe 999 GB",
		},
		{
			name:  "1000 GB rounds to '1 TB' (no trailing .0)",
			disks: []*Disk{mk("NVMe", 1000)},
			want:  "NVMe 1 TB",
		},
		{
			name:  "1500 GB → 1.5 TB",
			disks: []*Disk{mk("NVMe", 1500)},
			want:  "NVMe 1.5 TB",
		},
		{
			name:  "1920 GB (AX52 alt SKU) → 1.9 TB",
			disks: []*Disk{mk("NVMe", 1920)},
			want:  "NVMe 1.9 TB",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatDiskComposition(tc.disks))
		})
	}
}

func TestZFSPoolSubTreeAllocates(t *testing.T) {
	mk := func(diskType string, zfs int) *Disk {
		d := &Disk{Type: diskType, Size: 1000}
		d.Allocations.ZFS = zfs
		return d
	}

	t.Run("default 220 GB pool → 20 GB OpenEBS overflow", func(t *testing.T) {
		// Mirror: both disks have the same ZFS allocation; the
		// visible pool capacity = one disk's allocation.
		disks := []*Disk{mk("NVMe", 220), mk("NVMe", 220)}
		rendered := zfsPoolSubTree(disks).String()

		assert.Contains(t, rendered, `ZFS pool "primary" — mirror across 2 NVMe (OS: ext4 on RAID-1)`)
		assert.Contains(t, rendered, "/var/lib/containerd")
		assert.Contains(t, rendered, "100 GB")
		assert.Contains(t, rendered, "/var/log/pods")
		assert.Contains(t, rendered, " 50 GB")
		assert.Contains(t, rendered, "/var/lib/kubelet/pods")
		assert.Contains(t, rendered, "OpenEBS ZFS LocalPV")
		assert.Contains(t, rendered, " 20 GB free")
	})

	t.Run("operator-bumped 500 GB pool → 300 GB OpenEBS", func(t *testing.T) {
		disks := []*Disk{mk("NVMe", 500), mk("NVMe", 500)}
		rendered := zfsPoolSubTree(disks).String()

		// Reserved volumes don't move; only the OpenEBS remainder does.
		assert.Contains(t, rendered, "100 GB")
		assert.Contains(t, rendered, " 50 GB")
		assert.Contains(t, rendered, "300 GB free")
	})

	t.Run("under-sized pool → OpenEBS clamped to 0", func(t *testing.T) {
		// Defensive — should never happen in practice (the planner
		// rejects pools < 200 GB at config-validate time), but the
		// renderer shouldn't underflow into a negative size.
		disks := []*Disk{mk("NVMe", 100), mk("NVMe", 100)}
		rendered := zfsPoolSubTree(disks).String()
		assert.Contains(t, rendered, "  0 GB free")
	})

	t.Run("mixed disk types render shorthand in the header", func(t *testing.T) {
		disks := []*Disk{mk("NVMe", 220), mk("HDD", 220)}
		rendered := zfsPoolSubTree(disks).String()
		assert.Contains(t, rendered, "mirror across NVMe + HDD")
	})
}

// captureStdout runs fn while stdout is redirected to a pipe, then
// restores stdout and returns whatever fn wrote. Used by the
// PrettyPrint regression test — the function prints directly via
// fmt.Println rather than returning a string, so capturing is the
// cheapest way to assert on its output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	require.NoError(t, w.Close())
	<-done
	os.Stdout = orig
	return buf.String()
}
