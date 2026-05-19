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

// TestStoragePlansPrettyPrintCollapsesAlikeLayouts proves the
// collapsed-per-group rendering: a node-group with N alike plans
// shows the layout ONCE under a header that names every server it
// applies to, rather than repeating the disks N times.
func TestStoragePlansPrettyPrintCollapsesAlikeLayouts(t *testing.T) {
	mkPlan := func(serverID string) *StoragePlan {
		disk := &Disk{Name: "nvme0n1", Size: 1000}
		disk.Allocations.OS = 50
		disk.Allocations.ZFS = 220
		return &StoragePlan{
			ServerID: serverID,
			Disks:    []*Disk{disk},
		}
	}

	plans := StoragePlans{
		"control-plane": {mkPlan("100"), mkPlan("101"), mkPlan("102")},
		"workers":       {mkPlan("200")},
	}

	got := captureStdout(t, plans.PrettyPrint)

	// CP group: one disk layout, header naming all three CP servers.
	assert.Contains(t, got, "control-plane (3 servers: 100, 101, 102)")
	// Worker group: singular phrasing for the one server.
	assert.Contains(t, got, "workers (server 200)")
	// Disk allocation rendered once per group (not 3+1 = 4 times):
	assert.Equal(t, 2, strings.Count(got, "OS   : 50 GB"))
	assert.Equal(t, 2, strings.Count(got, "ZFS  : 220 GB"))
	// And the unallocated remainder.
	assert.Equal(t, 2, strings.Count(got, "nvme0n1 (730 GB unallocated)"))
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
