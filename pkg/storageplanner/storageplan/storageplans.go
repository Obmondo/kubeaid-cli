// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package storageplan

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

type (
	NodeGroupName = string

	StoragePlans map[NodeGroupName][]*StoragePlan
)

// controlPlaneGroup is the StoragePlans key for the control-plane node
// group — pinned first in the render order and exempt from the
// "NodeGroup:" header tag, since it isn't a worker pool.
const controlPlaneGroup = "control-plane"

// Geometry of the storage-layout box rendered by PrettyPrint.
const (
	// storageBoxTitle is embedded in the box's top border.
	storageBoxTitle = "Storage layout · KubeAid recommended"

	// storageBoxWarning is printed directly below the box rather than
	// inside it: the line is wider than any boxed content, so framing
	// it would force the whole box uncomfortably wide.
	storageBoxWarning = "Warning: KubeAid recommends this layout — " +
		"changing it destroys data + RAID/mirror redundancy"

	// storageBoxZFSNote states why the ZFS volumes carry no sizes:
	// they have no quota, so any figure would be a guess. Printed
	// below the box, under storageBoxWarning.
	storageBoxZFSNote = "Note: these volumes share the ZFS pool with " +
		"no quota — each grows into free space as needed."

	// Body-line indentation for the three nesting levels. The frame
	// adds one more leading space, so the operator sees 2 / 4 / 8
	// columns of indent for group headers / rows / sub-volume rows.
	indentHeader    = 1
	indentRow       = 3
	indentSubVolume = 7

	// Spacing between a data row's columns: label→size and size→note.
	gapLabelSize = 4
	gapSizeNote  = 3

	// Minimum spacing between a group name and its right-aligned
	// "N servers · disks" summary on a header line.
	minHeaderGap = 4
)

// PrettyPrint renders the storage layout the operator is about to
// approve as a single bordered box — one section per node group, each
// with an underlined uppercase header and a right-aligned size
// column — followed by a destructive-change warning.
//
// AreStoragePlansAlike enforces an identical layout across every
// server in a group, so the first plan is canonical and the box shows
// per-group facts, not per-server ones. Groups render in a stable
// order (control-plane first, then alphabetical) so re-running the
// command produces the same screen.
func (s StoragePlans) PrettyPrint() {
	if rendered := s.render(); rendered != "" {
		fmt.Println(rendered)
	}
}

// render builds the bordered storage-layout box plus the warning line
// and returns them as one string. Kept separate from PrettyPrint so
// tests can assert on the returned value instead of capturing stdout.
func (s StoragePlans) render() string {
	items := s.boxItems()
	if len(items) == 0 {
		return ""
	}

	border := lipgloss.RoundedBorder()

	// Pass 1 — size the shared label and size columns from the data
	// rows, so every row's size aligns in one column.
	labelCol, sizeWidth := 0, 0
	for _, it := range items {
		if it.kind != itemRow {
			continue
		}
		labelCol = max(labelCol, it.indent+utf8.RuneCountInString(it.label))
		sizeWidth = max(sizeWidth, utf8.RuneCountInString(it.size))
	}

	// Pass 2 — render every non-header body line and track the widest,
	// which sets the inner box width. Header lines right-align their
	// summary against that final width, so they are deferred to pass 3
	// and here only widen the box to fit their content.
	bodies := make([]string, len(items))
	innerWidth := utf8.RuneCountInString(storageBoxTitle) + 3 // top border must fit the title.
	for i, it := range items {
		switch it.kind {
		case itemRow:
			bodies[i] = rowBody(it, labelCol, sizeWidth)

		case itemRule:
			continue // rendered as a full-width rule in pass 3.

		case itemHeader:
			innerWidth = max(innerWidth, indentHeader+
				utf8.RuneCountInString(it.left)+minHeaderGap+
				utf8.RuneCountInString(it.right))
			continue

		case itemBlank:
			bodies[i] = ""
		}
		innerWidth = max(innerWidth, utf8.RuneCountInString(bodies[i]))
	}

	// Pass 3 — frame each line within the now-final inner width.
	var b strings.Builder
	b.WriteString(topBorder(border, innerWidth))
	for i, it := range items {
		b.WriteString("\n")

		line := bodies[i]
		switch it.kind {
		case itemRule:
			// A full-width rule inside the frame — divides a group's
			// header from its rows without breaking the box outline.
			line = strings.Repeat(border.Top, innerWidth)

		case itemHeader:
			line = headerBody(it, innerWidth)

		case itemBlank, itemRow:
			// line already holds the body rendered in pass 2.
		}
		b.WriteString(border.Left)
		b.WriteString(" ")
		b.WriteString(padRight(line, innerWidth))
		b.WriteString(" ")
		b.WriteString(border.Right)
	}
	b.WriteString("\n")
	b.WriteString(bottomBorder(border, innerWidth))

	// The captions below the box are colour-coded so the hard warning
	// and the soft note read apart at a glance: bold amber (ANSI 3)
	// for the warning, dimmed for the note.
	warningStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	noteStyle := lipgloss.NewStyle().Faint(true)
	b.WriteString("\n")
	b.WriteString(warningStyle.Render(storageBoxWarning))
	b.WriteString("\n")
	b.WriteString(noteStyle.Render(storageBoxZFSNote))
	return b.String()
}

type itemKind int

const (
	itemBlank itemKind = iota
	itemHeader
	itemRule
	itemRow
)

// boxItem is one logical line of the storage-layout box before it is
// framed by the border. kind selects which fields apply: a header
// carries left/right text, a rule has no fields (it renders as a
// full-width horizontal rule), and a data row carries an indent plus
// the label/size/note columns.
type boxItem struct {
	kind itemKind

	left, right string // itemHeader

	indent            int    // itemRow
	label, size, note string // itemRow
}

// boxItems flattens the storage plans into the ordered list of box
// lines: a leading blank, then per group a header, an underline rule,
// and the OS / ZFS pool / sub-volume / Rook Ceph rows, with a blank
// line separating groups.
func (s StoragePlans) boxItems() []boxItem {
	items := []boxItem{}
	for _, name := range s.sortedGroupNames() {
		plans := s[name]
		if len(plans) == 0 {
			continue
		}
		// A blank line above every group — including the first, where
		// it forms the box's top inner margin.
		items = append(items, boxItem{kind: itemBlank})
		items = append(items, groupItems(name, plans)...)
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// groupItems renders one node group: the header, its underline rule,
// and the OS / ZFS pool / sub-volume / Rook Ceph rows. The control
// plane gets a plain "Control plane" title; worker node groups are
// tagged "NodeGroup: <name>" with the name verbatim from general.yaml
// so the operator can match it against their config.
func groupItems(name string, plans []*StoragePlan) []boxItem {
	disks := plans[0].Disks

	header := "NodeGroup: " + name
	if name == controlPlaneGroup {
		header = "Control plane"
	}

	// The OS volume is a RAID-1 across only the disks that carry an OS
	// allocation — never every disk on the server. allocateStoragePlan
	// places it on exactly two disks; sizing it off len(disks) would
	// divide the mirror capacity by the wrong count on any server with
	// more than two disks (the rest back ZFS or CEPH, not the OS RAID).
	osTotal, osDiskCount := 0, 0
	for _, d := range disks {
		if d.Allocations.OS <= 0 {
			continue
		}
		osTotal += d.Allocations.OS
		osDiskCount++
	}

	items := []boxItem{
		{
			kind: itemHeader,
			left: header,
			right: fmt.Sprintf("%s · %s",
				serverCount(len(plans)), formatDiskComposition(disks)),
		},
		{kind: itemRule},
		{
			kind:   itemRow,
			indent: indentRow,
			label:  "OS volume",
			size:   formatDiskSize(mirroredSize(osTotal, osDiskCount)),
			note:   "ext4 · RAID-1",
		},
	}
	items = append(items, zfsRows(disks)...)

	// Rook Ceph OSDs — cephRows returns nil when no disk has CEPH space
	// (small SKUs fully consumed by OS + ZFS), so the section vanishes.
	items = append(items, cephRows(disks)...)
	return items
}

// zfsRows renders the ZFS pool header row and the mount points its
// volumes provide. The pool is a mirror, so its capacity is one
// disk's ZFS allocation — a real partition size, so it is shown. The
// mount points carry no size: the volumes have no ZFS quota, so any
// figure would be a guess.
func zfsRows(disks []*Disk) []boxItem {
	// The ZFS pool mirrors only the disks that carry a ZFS allocation —
	// a strict subset on a >2-disk server. Both the mirrored capacity
	// and the "mirror · …" disk shorthand must be derived from that
	// subset, not from every disk on the server.
	zfsDisks := make([]*Disk, 0, len(disks))
	zfsTotal := 0
	for _, d := range disks {
		if d.Allocations.ZFS <= 0 {
			continue
		}
		zfsDisks = append(zfsDisks, d)
		zfsTotal += d.Allocations.ZFS
	}
	capacity := mirroredSize(zfsTotal, len(zfsDisks))

	mountPoint := func(path string) boxItem {
		return boxItem{
			kind:   itemRow,
			indent: indentSubVolume,
			label:  "● " + path,
		}
	}

	return []boxItem{
		{
			kind:   itemRow,
			indent: indentRow,
			label:  `ZFS pool "primary"`,
			size:   formatDiskSize(capacity),
			note:   "mirror · " + formatDiskShorthand(zfsDisks),
		},
		mountPoint("/var/lib/containerd"),
		mountPoint("/var/log/pods"),
		mountPoint("/var/lib/kubelet/pods"),
	}
}

// cephRows renders the Rook Ceph OSD section: a header row carrying the
// total OSD count, then one indented sub-row per distinct disk profile
// — a {type, per-OSD size} pair — showing that profile's per-OSD size.
// Rook runs one OSD per disk, so the OSD count is the CEPH-disk count.
//
// Returns nil when no disk has CEPH space (small SKUs fully consumed by
// OS + ZFS); the caller then renders no Rook Ceph section at all.
func cephRows(disks []*Disk) []boxItem {
	type profile struct {
		diskType string
		alloc    int // allocated GB, not raw disk size.
	}
	order := []profile{}
	counts := map[profile]int{}
	for _, d := range disks {
		if d.Allocations.CEPH <= 0 {
			continue
		}
		p := profile{diskType: d.Type, alloc: d.Allocations.CEPH}
		if _, seen := counts[p]; !seen {
			order = append(order, p)
		}
		counts[p]++
	}
	if len(order) == 0 {
		return nil
	}

	osdCount := 0
	for _, p := range order {
		osdCount += counts[p]
	}
	note := "1 OSD"
	if osdCount > 1 {
		note = fmt.Sprintf("%d OSDs · one per disk", osdCount)
	}

	rows := []boxItem{{
		kind:   itemRow,
		indent: indentRow,
		label:  "Rook Ceph OSD",
		note:   note,
	}}
	for _, p := range order {
		shorthand := p.diskType
		if counts[p] > 1 {
			shorthand = fmt.Sprintf("%d × %s", counts[p], p.diskType)
		}
		rows = append(rows, boxItem{
			kind:   itemRow,
			indent: indentSubVolume,
			label:  "● " + shorthand,
			size:   formatDiskSize(p.alloc),
		})
	}
	return rows
}

// serverCount renders a node-group server tally with correct
// pluralisation: "1 server", "3 servers".
func serverCount(n int) string {
	if n == 1 {
		return "1 server"
	}
	return fmt.Sprintf("%d servers", n)
}

// mirroredSize converts a per-disk allocation summed across a mirror
// back to the operator-visible capacity. Both disks hold identical
// copies, so usable space is one disk's allocation — total / count.
// Returns 0 for an empty group rather than dividing by zero.
func mirroredSize(total, diskCount int) int {
	if diskCount <= 0 {
		return 0
	}
	return total / diskCount
}

// sortedGroupNames returns the group names in the rendering order:
// "control-plane" first (when present), then any remaining groups
// alphabetically. Stable output across runs matters because the
// operator reviews the plan against their general.yaml and a shifting
// order would create false-positive diffs.
func (s StoragePlans) sortedGroupNames() []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == controlPlaneGroup {
			return true
		}
		if names[j] == controlPlaneGroup {
			return false
		}
		return names[i] < names[j]
	})
	return names
}

// formatDiskComposition collapses N disks of the same Type + Size
// into "N × Type Size", joins distinct types with " + ":
//
//	[NVMe 1TB, NVMe 1TB]          → "2 × NVMe 1 TB"
//	[NVMe 1TB, HDD 4TB]           → "NVMe 1 TB + HDD 4 TB"
//	[HDD 2TB]                     → "HDD 2 TB"
//	[NVMe 1TB, NVMe 1TB, HDD 4TB] → "2 × NVMe 1 TB + HDD 4 TB"
//
// Order in the output preserves first-encountered order of the
// (Type, Size) tuple across the input slice — matches lsblk's natural
// reporting order so the operator sees the disks in the same shape
// they'd see in the Hetzner Robot UI.
func formatDiskComposition(disks []*Disk) string {
	type key struct {
		Type string
		Size int
	}
	counts := map[key]int{}
	order := make([]key, 0, len(disks))
	for _, d := range disks {
		k := key{Type: d.Type, Size: d.Size}
		if _, seen := counts[k]; !seen {
			order = append(order, k)
		}
		counts[k]++
	}

	parts := make([]string, 0, len(order))
	for _, k := range order {
		part := fmt.Sprintf("%s %s", k.Type, formatDiskSize(k.Size))
		if counts[k] > 1 {
			part = fmt.Sprintf("%d × %s", counts[k], part)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " + ")
}

// formatDiskShorthand renders a no-sizes variant — "2 NVMe" or
// "NVMe + HDD" — used in the ZFS pool note where the disk sizes have
// already been shown on the header line. The note reads naturally as
// "mirror · 2 NVMe" rather than "mirror · 2 × NVMe 1 TB".
func formatDiskShorthand(disks []*Disk) string {
	counts := map[string]int{}
	order := make([]string, 0, len(disks))
	for _, d := range disks {
		if _, seen := counts[d.Type]; !seen {
			order = append(order, d.Type)
		}
		counts[d.Type]++
	}
	parts := make([]string, 0, len(order))
	for _, t := range order {
		if counts[t] > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[t], t))
			continue
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, " + ")
}

// formatDiskSize renders a disk size in GB as a human label:
// integer GB up to 999 ("999 GB"), then TB above that ("1 TB",
// "1.5 TB", "1.7 TB"). Sub-TB sizes stay in GB because operators
// remember Hetzner offerings in GB ("Hetzner AX42 ships 1 TB", "AX52
// ships 1 TB or 1.92 TB") and a 100 GB OS-only NVMe should read as
// "100 GB", not "0.1 TB".
func formatDiskSize(gb int) string {
	if gb < 1000 {
		return fmt.Sprintf("%d GB", gb)
	}
	tb := float64(gb) / 1000.0
	// Round to 1 decimal place; trim a trailing ".0" so "1000 GB" → "1 TB".
	rounded := fmt.Sprintf("%.1f", tb)
	rounded = strings.TrimSuffix(rounded, ".0")
	return rounded + " TB"
}

// rowBody renders a data row: the indented label padded to the shared
// label column, the right-aligned size column, then the optional
// note. A row with an empty size still consumes the size column so
// the note stays aligned with notes on sized rows.
func rowBody(it boxItem, labelCol, sizeWidth int) string {
	label := strings.Repeat(" ", it.indent) + it.label
	line := padRight(label, labelCol) +
		strings.Repeat(" ", gapLabelSize) +
		padLeft(it.size, sizeWidth)
	if it.note != "" {
		line += strings.Repeat(" ", gapSizeNote) + it.note
	}
	return line
}

// headerBody renders a group header: the uppercase group name on the
// left, the "N servers · disks" summary right-aligned against the
// inner box width.
func headerBody(it boxItem, innerWidth int) string {
	left := strings.Repeat(" ", indentHeader) + it.left
	gap := max(
		innerWidth-utf8.RuneCountInString(left)-utf8.RuneCountInString(it.right),
		minHeaderGap,
	)
	return left + strings.Repeat(" ", gap) + it.right
}

// topBorder renders the box's top edge with the title embedded:
// "╭─ <title> ─────╮", sized so the whole edge spans innerWidth + 4
// (the body plus its one-space margins plus the two corners).
func topBorder(b lipgloss.Border, innerWidth int) string {
	titled := b.Top + " " + storageBoxTitle + " "
	fill := max((innerWidth+2)-utf8.RuneCountInString(titled), 0)
	return b.TopLeft + titled + strings.Repeat(b.Top, fill) + b.TopRight
}

// bottomBorder renders the box's plain bottom edge.
func bottomBorder(b lipgloss.Border, innerWidth int) string {
	return b.BottomLeft + strings.Repeat(b.Bottom, innerWidth+2) + b.BottomRight
}

// padRight pads s with trailing spaces to width display columns. All
// box content is single-width runes, so a rune count is the column
// count; fmt's "%-*s" cannot be used because it pads by byte length
// and the content carries multi-byte runes (·, ×, ─).
func padRight(s string, width int) string {
	if pad := width - utf8.RuneCountInString(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// padLeft pads s with leading spaces to width display columns, used
// to right-align the size column. See padRight on why "%*s" is unfit.
func padLeft(s string, width int) string {
	if pad := width - utf8.RuneCountInString(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

func (s *StoragePlans) GetApproval(ctx context.Context) {
	// Pause the bar around the print + prompt. The bar's 100ms auto-
	// render goroutine otherwise re-paints "<spinner> [elapsed] " at
	// column 0 on top of our box and (worse) on top of the "Proceed
	// with the above storage plans?" prompt — the operator sees the
	// prompt with its first few characters clipped. Same pattern as
	// dns_wait.go.
	//
	// No defer for Resume because the "no" branch calls os.Exit(1)
	// directly, after which deferred funcs would not run. Explicit
	// Resume at both exit points keeps the lifecycle obvious.
	bar := progress.FromCtx(ctx)
	bar.Pause()

	s.PrettyPrint()

	for {
		fmt.Print(lipgloss.NewStyle().Render("Proceed with the above storage plans? (yes/no): "))

		response, err := bufio.NewReader(os.Stdin).ReadString('\n')
		assert.AssertErrNil(ctx, err, "Failed reading user input")

		switch strings.TrimSpace(strings.ToLower(response)) {
		case "yes":
			bar.Resume()
			return

		case "no":
			slog.ErrorContext(ctx, "Storage plans not approved")
			bar.Resume()
			os.Exit(1)
		}
	}
}

/*
By alikeness, I mean, the 2 disks across which the ZFS pool will be running, must be the same
across all the nodes in the node-group. This makes the command to create a ZFS pool to be the same
across the nodes, for e.g. :

	zpool create primary mirror /dev/nvme0n1 /dev/nvme1n1

For all the nodes in a node-group, we have a single KubeadmControlPlane / KubeadmConfig resource.
And the ZFS pool creation command goes in the postKubeadm section of that resource. So, it must be
same for all the nodes.
*/
func AreStoragePlansAlike(storagePlans []*StoragePlan) bool {
	var referenceDisks []*Disk
	for i, storagePlan := range storagePlans {
		if i == 0 {
			referenceDisks = storagePlan.ZFS
			continue
		}

		if len(storagePlan.ZFS) != len(referenceDisks) {
			return false
		}
		for j, disk := range storagePlan.ZFS {
			if referenceDisks[j].Name != disk.Name {
				return false
			}
		}
	}

	return true
}
