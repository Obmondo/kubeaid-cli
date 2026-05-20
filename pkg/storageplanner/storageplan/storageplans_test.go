// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
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

// mkRenderPlan builds a StoragePlan with two identical disks — the
// typical 2-disk AX-series server shape. The OS volume (50 GB) and
// ZFS pool (the default size) allocations are fixed; the raw disk
// size and the per-disk Rook Ceph allocation are the varied inputs.
func mkRenderPlan(serverID, diskType string, size, ceph int) *StoragePlan {
	mk := func(name string) *Disk {
		d := &Disk{Name: name, Type: diskType, Size: size}
		d.Allocations.OS = 50                            // a 50 GB RAID-1 OS volume.
		d.Allocations.ZFS = constants.ZFSPoolDefaultSize // the default-sized ZFS pool.
		d.Allocations.CEPH = ceph
		return d
	}
	return &StoragePlan{
		ServerID: serverID,
		Disks:    []*Disk{mk("nvme0n1"), mk("nvme1n1")},
	}
}

// TestStoragePlansRender proves the storage layout renders as a single
// bordered box: one section per node group, an underlined uppercase
// header, the OS / ZFS / sub-volume / Rook Ceph rows, and a
// destructive-change warning below the box.
func TestStoragePlansRender(t *testing.T) {
	plans := StoragePlans{
		"control-plane": {
			mkRenderPlan("100", "NVMe", 474, 204),
			mkRenderPlan("101", "NVMe", 474, 204),
			mkRenderPlan("102", "NVMe", 474, 204),
		},
		"gpu-workers": {
			mkRenderPlan("200", "NVMe", 1900, 1630),
			mkRenderPlan("201", "NVMe", 1900, 1630),
		},
		"workers": {
			mkRenderPlan("300", "HDD", 1900, 1630),
			mkRenderPlan("301", "HDD", 1900, 1630),
			mkRenderPlan("302", "HDD", 1900, 1630),
		},
	}

	got := plans.render()
	t.Logf("rendered storage layout:\n%s", got)

	// Box framing — title in the top border, rounded corners.
	assert.Contains(t, got, "Storage layout · KubeAid recommended")
	assert.Contains(t, got, "╭")
	assert.Contains(t, got, "╮")
	assert.Contains(t, got, "╰")
	assert.Contains(t, got, "╯")

	// Each node group is its own section: a header, a full-width rule,
	// then its rows. The control plane gets a plain title; worker node
	// groups are tagged "NodeGroup: <name>" with the verbatim name.
	assert.Contains(t, got, "Control plane")
	assert.Contains(t, got, "NodeGroup: gpu-workers")
	assert.Contains(t, got, "NodeGroup: workers")
	assert.NotContains(t, got, "NodeGroup: control-plane")
	assert.NotContains(t, got, "CONTROL-PLANE") // group names are no longer upper-cased
	assert.Contains(t, got, "3 servers · 2 × NVMe 474 GB")
	assert.Contains(t, got, "2 servers · 2 × NVMe 1.9 TB")
	assert.Contains(t, got, "3 servers · 2 × HDD 1.9 TB")
	// One full-width rule per group, inside the frame — no ├┤ crossbar.
	assert.NotContains(t, got, "├")
	assert.NotContains(t, got, "┤")
	assert.Equal(t, 3, strings.Count(got, "│ ─"))

	// OS volume row.
	assert.Contains(t, got, "OS volume")
	assert.Contains(t, got, "ext4 · RAID-1")

	// ZFS pool row keeps its real size; its mount points are listed
	// beneath it as a bulleted list, one set per group.
	assert.Contains(t, got, `ZFS pool "primary"`)
	assert.Contains(t, got, "220 GB") // the pool's real partition size
	assert.Contains(t, got, "mirror · 2 NVMe")
	assert.Contains(t, got, "mirror · 2 HDD")
	assert.Equal(t, 3, strings.Count(got, "● /var/lib/containerd"))
	assert.Equal(t, 3, strings.Count(got, "● /var/log/pods"))
	assert.Equal(t, 3, strings.Count(got, "● /var/lib/kubelet/pods"))
	// pod-ephemeral and OpenEBS LocalPV are no longer listed, and the
	// estimated per-volume sizes (and their "~" prefix) are gone.
	assert.NotContains(t, got, "pod ephemeral")
	assert.NotContains(t, got, "OpenEBS")
	assert.NotContains(t, got, "~")

	// Rook Ceph OSD section — a header carrying the per-group OSD count,
	// then one indented sub-row per disk type with its per-OSD size.
	assert.Equal(t, 3, strings.Count(got, "Rook Ceph OSD"))
	assert.Equal(t, 3, strings.Count(got, "2 OSDs · one per disk"))
	assert.Equal(t, 2, strings.Count(got, "● 2 × NVMe")) // control-plane + gpu-workers
	assert.Equal(t, 1, strings.Count(got, "● 2 × HDD"))  // workers
	assert.Contains(t, got, "204 GB")                    // control-plane CEPH alloc
	assert.Contains(t, got, "1.6 TB")                    // worker CEPH alloc

	// The warning and the ZFS-quota note sit on their own lines below
	// the box.
	assert.Contains(t, got, storageBoxWarning)
	assert.Contains(t, got, storageBoxZFSNote)

	// Server IDs are not leaked into the header — the previous shape
	// printed "(3 servers: 100, 101, 102)"; the box shows just the count.
	assert.NotContains(t, got, "100, 101")
	assert.NotContains(t, got, "(server ")

	// Groups render control-plane first, then alphabetical.
	cpAt := strings.Index(got, "Control plane")
	gpuAt := strings.Index(got, "NodeGroup: gpu-workers")
	workersAt := strings.Index(got, "NodeGroup: workers")
	require.True(t, cpAt >= 0 && gpuAt >= 0 && workersAt >= 0)
	assert.Less(t, cpAt, gpuAt, "control-plane before gpu-workers")
	assert.Less(t, gpuAt, workersAt, "gpu-workers before workers")

	// Every box line is exactly the same display width — the frame
	// and the crossbar only line up if the column maths is right.
	lines := strings.Split(got, "\n")
	require.Greater(t, len(lines), 3)
	boxLines := lines[:len(lines)-2] // the last two lines are the warning + ZFS note.
	width := utf8.RuneCountInString(boxLines[0])
	for i, line := range boxLines {
		assert.Equal(t, width, utf8.RuneCountInString(line),
			"box line %d has a mismatched width: %q", i, line)
	}
	assert.True(t, strings.HasPrefix(boxLines[0], "╭"), "first line is the top border")
	assert.True(t, strings.HasPrefix(boxLines[len(boxLines)-1], "╰"), "last box line is the bottom border")
}

func TestStoragePlansRenderOmitsCephWhenUnallocated(t *testing.T) {
	// A small SKU whose disks are fully consumed by OS + ZFS leaves no
	// CEPH space — the Rook Ceph row must not render.
	plans := StoragePlans{
		"control-plane": {mkRenderPlan("100", "NVMe", 270, 0)},
	}
	got := plans.render()

	assert.NotContains(t, got, "Rook Ceph OSD")
	assert.Contains(t, got, "OS volume")
	assert.Contains(t, got, `ZFS pool "primary"`)
}

func TestStoragePlansRenderEmpty(t *testing.T) {
	assert.Empty(t, StoragePlans{}.render(),
		"no node groups should render nothing, not an empty box")
}

func TestSortedGroupNames(t *testing.T) {
	t.Run("control-plane pinned first, rest alphabetical", func(t *testing.T) {
		plans := StoragePlans{
			"workers":       {},
			"gpu-workers":   {},
			"control-plane": {},
			"infra":         {},
		}
		got := plans.sortedGroupNames()
		assert.Equal(t, []string{"control-plane", "gpu-workers", "infra", "workers"}, got)
	})
	t.Run("no control-plane → pure alphabetical", func(t *testing.T) {
		plans := StoragePlans{"workers": {}, "infra": {}}
		assert.Equal(t, []string{"infra", "workers"}, plans.sortedGroupNames())
	})
}

func TestServerCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "1 server"},
		{2, "2 servers"},
		{3, "3 servers"},
		{0, "0 servers"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, serverCount(tc.n))
	}
}

func TestMirroredSize(t *testing.T) {
	cases := []struct {
		name      string
		total     int
		diskCount int
		want      int
	}{
		{"two-disk mirror halves the summed allocation", 100, 2, 50},
		{"default ZFS pool across two disks", 440, 2, 220},
		{"single disk returns the allocation as-is", 220, 1, 220},
		{"zero disks does not divide by zero", 0, 0, 0},
		{"three-disk group", 300, 3, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mirroredSize(tc.total, tc.diskCount))
		})
	}
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

func TestFormatDiskShorthand(t *testing.T) {
	mk := func(diskType string) *Disk { return &Disk{Type: diskType} }
	cases := []struct {
		name  string
		disks []*Disk
		want  string
	}{
		{
			name:  "two identical disks collapse with a count",
			disks: []*Disk{mk("NVMe"), mk("NVMe")},
			want:  "2 NVMe",
		},
		{
			name:  "single disk → bare type",
			disks: []*Disk{mk("HDD")},
			want:  "HDD",
		},
		{
			name:  "heterogeneous types joined with ' + '",
			disks: []*Disk{mk("NVMe"), mk("HDD")},
			want:  "NVMe + HDD",
		},
		{
			name:  "mixed multiplicities → only the repeated type counted",
			disks: []*Disk{mk("NVMe"), mk("NVMe"), mk("HDD")},
			want:  "2 NVMe + HDD",
		},
		{
			name:  "empty input",
			disks: []*Disk{},
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatDiskShorthand(tc.disks))
		})
	}
}

func TestCephRows(t *testing.T) {
	mk := func(diskType string, cephAlloc int) *Disk {
		d := &Disk{Type: diskType}
		d.Allocations.CEPH = cephAlloc
		return d
	}
	cases := []struct {
		name  string
		disks []*Disk
		want  []boxItem
	}{
		{
			name:  "uniform two-disk group → header + one sub-row",
			disks: []*Disk{mk("NVMe", 730), mk("NVMe", 730)},
			want: []boxItem{
				{kind: itemRow, indent: indentRow, label: "Rook Ceph OSD", note: "2 OSDs · one per disk"},
				{kind: itemRow, indent: indentSubVolume, label: "● 2 × NVMe", size: "730 GB"},
			},
		},
		{
			name:  "heterogeneous server → a sub-row per disk type",
			disks: []*Disk{mk("NVMe", 500), mk("NVMe", 500), mk("HDD", 1500), mk("HDD", 1500)},
			want: []boxItem{
				{kind: itemRow, indent: indentRow, label: "Rook Ceph OSD", note: "4 OSDs · one per disk"},
				{kind: itemRow, indent: indentSubVolume, label: "● 2 × NVMe", size: "500 GB"},
				{kind: itemRow, indent: indentSubVolume, label: "● 2 × HDD", size: "1.5 TB"},
			},
		},
		{
			name:  "same type, differing OSD sizes → a sub-row each",
			disks: []*Disk{mk("HDD", 1730), mk("HDD", 1730), mk("HDD", 2000), mk("HDD", 2000)},
			want: []boxItem{
				{kind: itemRow, indent: indentRow, label: "Rook Ceph OSD", note: "4 OSDs · one per disk"},
				{kind: itemRow, indent: indentSubVolume, label: "● 2 × HDD", size: "1.7 TB"},
				{kind: itemRow, indent: indentSubVolume, label: "● 2 × HDD", size: "2 TB"},
			},
		},
		{
			name:  "different types but equal alloc → still split per type",
			disks: []*Disk{mk("NVMe", 500), mk("HDD", 500)},
			want: []boxItem{
				{kind: itemRow, indent: indentRow, label: "Rook Ceph OSD", note: "2 OSDs · one per disk"},
				{kind: itemRow, indent: indentSubVolume, label: "● NVMe", size: "500 GB"},
				{kind: itemRow, indent: indentSubVolume, label: "● HDD", size: "500 GB"},
			},
		},
		{
			name:  "single CEPH disk → 1 OSD, no count prefix on the sub-row",
			disks: []*Disk{mk("NVMe", 500)},
			want: []boxItem{
				{kind: itemRow, indent: indentRow, label: "Rook Ceph OSD", note: "1 OSD"},
				{kind: itemRow, indent: indentSubVolume, label: "● NVMe", size: "500 GB"},
			},
		},
		{
			name:  "mixed alloc-vs-no-alloc → only allocated disks counted",
			disks: []*Disk{mk("NVMe", 0), mk("NVMe", 500)},
			want: []boxItem{
				{kind: itemRow, indent: indentRow, label: "Rook Ceph OSD", note: "1 OSD"},
				{kind: itemRow, indent: indentSubVolume, label: "● NVMe", size: "500 GB"},
			},
		},
		{
			name:  "disks without CEPH allocation → no rows",
			disks: []*Disk{mk("NVMe", 0), mk("NVMe", 0)},
			want:  nil,
		},
		{
			name:  "empty input → no rows",
			disks: []*Disk{},
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, cephRows(tc.disks))
		})
	}
}

func TestZFSRows(t *testing.T) {
	mk := func(diskType string, zfs int) *Disk {
		d := &Disk{Type: diskType}
		d.Allocations.ZFS = zfs
		return d
	}
	// find returns the row carrying label, failing the test if absent.
	find := func(rows []boxItem, label string) boxItem {
		for _, r := range rows {
			if r.label == label {
				return r
			}
		}
		t.Fatalf("zfsRows produced no %q row", label)
		return boxItem{}
	}

	t.Run("pool keeps its real size; mount points listed beneath it", func(t *testing.T) {
		// Mirror: each disk carries the full pool allocation; capacity
		// is one disk's worth — a real partition size, so it shows.
		rows := zfsRows([]*Disk{mk("NVMe", 220), mk("NVMe", 220)})

		pool := find(rows, `ZFS pool "primary"`)
		assert.Equal(t, "220 GB", pool.size)
		assert.Equal(t, "mirror · 2 NVMe", pool.note)

		// The mount points carry no size — the volumes have no quota,
		// so a figure would just be a guess.
		for _, label := range []string{
			"● /var/lib/containerd",
			"● /var/log/pods",
			"● /var/lib/kubelet/pods",
		} {
			row := find(rows, label)
			assert.Empty(t, row.size, "%s must carry no size", label)
			assert.Empty(t, row.note)
		}
	})

	t.Run("pool size tracks the operator's bareMetal.zfs.size", func(t *testing.T) {
		rows := zfsRows([]*Disk{mk("NVMe", 500), mk("NVMe", 500)})
		assert.Equal(t, "500 GB", find(rows, `ZFS pool "primary"`).size)
	})

	t.Run("mixed disk types render shorthand in the pool note", func(t *testing.T) {
		rows := zfsRows([]*Disk{mk("NVMe", 220), mk("HDD", 220)})
		assert.Equal(t, "mirror · NVMe + HDD", find(rows, `ZFS pool "primary"`).note)
	})
}

// TestGroupItemsSizesMirrorByMirrorWidth proves the OS volume and ZFS
// pool rows size their mirror from the disks that actually back that
// mirror — not from every disk on the server. allocateStoragePlan
// places the OS RAID-1 and the ZFS mirror on exactly two disks each,
// so on a server with more than two disks the rest carry no OS/ZFS
// allocation; dividing the summed allocation by the full disk count
// would halve (or worse) the reported capacity.
func TestGroupItemsSizesMirrorByMirrorWidth(t *testing.T) {
	mk := func(name, diskType string, size, os, zfs, ceph int) *Disk {
		d := &Disk{Name: name, Type: diskType, Size: size}
		d.Allocations.OS = os
		d.Allocations.ZFS = zfs
		d.Allocations.CEPH = ceph
		return d
	}
	find := func(items []boxItem, label string) boxItem {
		for _, it := range items {
			if it.label == label {
				return it
			}
		}
		t.Fatalf("groupItems produced no %q row", label)
		return boxItem{}
	}

	tests := []struct {
		name        string
		disks       []*Disk
		wantOSSize  string
		wantZFSSize string
		wantZFSNote string
	}{
		{
			// A 2× NVMe + 2× HDD server: by the priority scores the OS
			// RAID-1 lands on the HDDs and the ZFS mirror on the NVMes —
			// two disjoint two-disk mirrors.
			name: "2 NVMe + 2 HDD — disjoint OS and ZFS mirrors",
			disks: []*Disk{
				mk("nvme0n1", "NVMe", 500, 0, 300, 200),
				mk("nvme1n1", "NVMe", 500, 0, 300, 200),
				mk("sda", "HDD", 1000, 50, 0, 950),
				mk("sdb", "HDD", 1000, 50, 0, 950),
			},
			wantOSSize:  "50 GB",
			wantZFSSize: "300 GB",
			wantZFSNote: "mirror · 2 NVMe",
		},
		{
			// Four identical HDDs: OS and ZFS share the first two disks;
			// the other two are CEPH-only.
			name: "4 identical HDDs — two disks are CEPH-only",
			disks: []*Disk{
				mk("sda", "HDD", 500, 50, 220, 230),
				mk("sdb", "HDD", 500, 50, 220, 230),
				mk("sdc", "HDD", 500, 0, 0, 500),
				mk("sdd", "HDD", 500, 0, 0, 500),
			},
			wantOSSize:  "50 GB",
			wantZFSSize: "220 GB",
			wantZFSNote: "mirror · 2 HDD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := groupItems(controlPlaneGroup, []*StoragePlan{{Disks: tc.disks}})

			osRow := find(items, "OS volume")
			assert.Equal(t, tc.wantOSSize, osRow.size, "OS volume size")

			zfsRow := find(items, `ZFS pool "primary"`)
			assert.Equal(t, tc.wantZFSSize, zfsRow.size, "ZFS pool size")
			assert.Equal(t, tc.wantZFSNote, zfsRow.note, "ZFS pool note")
		})
	}
}

// TestStoragePlansRenderSplitsCephByDiskType proves a heterogeneous
// server renders the Rook Ceph OSD section as a header carrying the
// total OSD count plus one indented sub-row per disk type — the NVMe
// sub-row above the HDD one.
func TestStoragePlansRenderSplitsCephByDiskType(t *testing.T) {
	mk := func(name, diskType string, size, os, zfs, ceph int) *Disk {
		d := &Disk{Name: name, Type: diskType, Size: size}
		d.Allocations.OS, d.Allocations.ZFS, d.Allocations.CEPH = os, zfs, ceph
		return d
	}
	plans := StoragePlans{"control-plane": {{Disks: []*Disk{
		mk("nvme0n1", "NVMe", 1900, 0, 220, 1680),
		mk("nvme1n1", "NVMe", 1900, 0, 220, 1680),
		mk("sda", "HDD", 4000, 50, 0, 3950),
		mk("sdb", "HDD", 4000, 50, 0, 3950),
	}}}}

	got := plans.render()
	t.Logf("rendered storage layout:\n%s", got)

	// One Rook Ceph OSD header, the total OSD count beside it.
	assert.Contains(t, got, "Rook Ceph OSD")
	assert.Contains(t, got, "4 OSDs · one per disk")

	// One indented sub-row per disk type, each carrying its OSD size.
	assert.Contains(t, got, "● 2 × NVMe")
	assert.Contains(t, got, "● 2 × HDD")
	assert.Contains(t, got, "1.7 TB") // per-NVMe-OSD size (1680 GB)

	// Header sits above the NVMe sub-row, which sits above the HDD one.
	header := strings.Index(got, "Rook Ceph OSD")
	nvme := strings.Index(got, "● 2 × NVMe")
	hdd := strings.Index(got, "● 2 × HDD")
	require.True(t, header >= 0 && nvme >= 0 && hdd >= 0)
	assert.Less(t, header, nvme, "Rook Ceph OSD header above the NVMe sub-row")
	assert.Less(t, nvme, hdd, "NVMe sub-row above the HDD sub-row")
}
