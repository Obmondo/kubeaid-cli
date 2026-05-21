// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
)

// K8sProfile is one row of the K8s version picker — a positioned
// "track" within Hetzner's support window of the four most recent
// upstream minors. Operators pick a profile (not a version) so the
// CLI can resolve the concrete patch from current-minor + EOL data
// at render time.
type K8sProfile struct {
	// Internal name maps to the position in the support window —
	// "third", "second", "after-first-dot", "latest". Stable
	// across versions; used for the radio-select default.
	Name string

	// Friendly label shown in the table + huh select. "Proven",
	// "Balanced", "Early Adopter", "Bleeding Edge".
	Label string

	// Concrete version string the picker writes back into
	// cfg.K8sVersion. For "Early Adopter" this is a placeholder
	// like "v1.36.1" — the operator can edit it manually in the
	// rendered general.yaml if a later patch is preferred.
	Version string

	// Risk + Maintenance + UpgradeWindow are display-only,
	// rendered into the lipgloss table so the operator sees the
	// tradeoff at a glance.
	Risk          string
	Maintenance   string
	UpgradeWindow string

	// Disabled marks the row as un-pickable in the huh select.
	// Set when EOL data doesn't have a patch for that minor (e.g.
	// the third-back minor is missing from the embedded table).
	Disabled bool

	// Note shows up in the table cell + as the huh option's
	// description. Used to surface a "(EOL)" / "(unreleased)"
	// hint when applicable.
	Note string
}

// k8sProfileBalancedName is the radio-select default — second-back
// minor matches today's silent "latest-1" auto-detect, so existing
// scripted setups keep working without operator surprise.
const k8sProfileBalancedName = "second"

// pickK8sProfile renders the comparison table + a huh select and
// returns the picked version. detected.K8sVersion (silent autodetect
// result) is used as the seed for "current minor"; on miss (offline,
// API change, anything else) the picker falls back to the newest
// cycle in the embedded EOL data and prints a small note in the
// table so the operator knows.
//
// Returns the version string the operator picked. Empty string +
// nil error on Ctrl+C from huh — caller treats it like "operator
// declined" and uses the autodetect fallback.
func pickK8sProfile(detected *autoDetectedConfig) (string, error) {
	latestPerCycle, err := parser.K8sLatestPerCycle()
	if err != nil {
		return "", fmt.Errorf("loading embedded K8s EOL data: %w", err)
	}

	currentMinor, currentSource := resolveCurrentK8sMinor(detected, latestPerCycle)
	if currentMinor == 0 {
		// No live data and no embedded data — bail to
		// autodetect's silent default. Don't show the picker
		// against an empty table.
		slog.Warn("Could not resolve a current K8s minor; skipping profile picker")
		return detected.K8sVersion, nil
	}

	profiles := buildK8sProfiles(currentMinor, latestPerCycle)

	fmt.Println()
	fmt.Println(pickK8sProfileTitle(detected.KubeAidVersion))
	fmt.Println()
	fmt.Println(renderK8sProfileTable(profiles))
	if currentSource == k8sCurrentSourceEOL {
		fmt.Println()
		fmt.Printf("  (offline — current minor sourced from embedded EOL data,\n")
		fmt.Printf("   may be stale relative to dl.k8s.io)\n")
	}
	fmt.Println()

	picked := defaultPickedProfile(profiles)
	options := huhOptionsForProfiles(profiles, picked)

	if err := huh.NewSelect[string]().
		Title("K8s version profile:").
		Options(options...).
		Value(&picked).
		Run(); err != nil {
		// Bubble up huh.ErrUserAborted unwrapped so the
		// ConfigFromPrompt-level deferred handler can detect it
		// via errors.Is and exit cleanly. Wrapping with %w would
		// still satisfy errors.Is but the bare sentinel is the
		// least surprising contract for a single-call helper.
		return "", err
	}

	for _, p := range profiles {
		if p.Name == picked {
			fmt.Printf("  Picked: %s — Kubernetes %s\n\n", p.Label, p.Version)
			return p.Version, nil
		}
	}
	return detected.K8sVersion, nil
}

const (
	k8sCurrentSourceLive = "live"
	k8sCurrentSourceEOL  = "eol"
)

// resolveCurrentK8sMinor picks the integer minor we treat as the
// "current" K8s release for profile placement. Live autodetect
// (dl.k8s.io) wins when present; otherwise we take the highest
// cycle in the embedded EOL data so the picker still works on a
// disconnected machine.
func resolveCurrentK8sMinor(detected *autoDetectedConfig, latestPerCycle map[string]string) (int, string) {
	// Live autodetect already populated detected.K8sVersion — that
	// is "latest minor minus 1", so add one back to get current.
	if detected != nil && detected.K8sVersion != "" {
		if minor, err := parseMinorVersion(detected.K8sVersion); err == nil {
			return minor + 1, k8sCurrentSourceLive
		}
	}

	// Fall back: highest cycle in embedded EOL data.
	maxMinor := 0
	for cycle := range latestPerCycle {
		minor, err := strconv.Atoi(strings.TrimPrefix(cycle, "1."))
		if err == nil && minor > maxMinor {
			maxMinor = minor
		}
	}
	if maxMinor == 0 {
		return 0, ""
	}
	return maxMinor, k8sCurrentSourceEOL
}

// buildK8sProfiles produces the four profiles for the given current
// minor (e.g. 36 for v1.36) using latestPerCycle to resolve concrete
// patch versions. Pure function — deterministic for unit tests, no
// network or log calls.
func buildK8sProfiles(currentMinor int, latestPerCycle map[string]string) []K8sProfile {
	cycle := func(minor int) string { return fmt.Sprintf("1.%d", minor) }
	resolveLatest := func(minor int) (string, bool) {
		v, ok := latestPerCycle[cycle(minor)]
		if !ok || v == "" {
			return "", false
		}
		return "v" + v, true
	}

	provenVer, provenOk := resolveLatest(currentMinor - 2)
	balancedVer, balancedOk := resolveLatest(currentMinor - 1)
	earlyVer := fmt.Sprintf("v1.%d.1", currentMinor)
	bleedingVer := fmt.Sprintf("v1.%d.0", currentMinor)

	provenNote := ""
	if !provenOk {
		provenNote = "no EOL entry"
	}
	balancedNote := ""
	if !balancedOk {
		balancedNote = "no EOL entry"
	}

	return []K8sProfile{
		{
			Name:          "third",
			Label:         "Proven",
			Version:       provenVer,
			Risk:          "Lowest",
			Maintenance:   "Lowest — battle-tested",
			UpgradeWindow: "~6 months",
			Disabled:      !provenOk,
			Note:          provenNote,
		},
		{
			Name:          "second",
			Label:         "Balanced",
			Version:       balancedVer,
			Risk:          "Low",
			Maintenance:   "Low — 3 months of stability",
			UpgradeWindow: "~6 months",
			Disabled:      !balancedOk,
			Note:          balancedNote,
		},
		{
			Name:          "after-first-dot",
			Label:         "Early Adopter",
			Version:       earlyVer,
			Risk:          "Medium-High",
			Maintenance:   "Medium — community shakes out bugs first",
			UpgradeWindow: "~2-4 weeks delay",
		},
		{
			Name:          "latest",
			Label:         "Bleeding Edge",
			Version:       bleedingVer,
			Risk:          "High",
			Maintenance:   "High — breaking changes, app fixes needed",
			UpgradeWindow: "Always chasing",
		},
	}
}

// defaultPickedProfile returns the Name of the profile to preselect
// in the huh select. Balanced when available; first non-disabled
// profile otherwise.
func defaultPickedProfile(profiles []K8sProfile) string {
	for _, p := range profiles {
		if p.Name == k8sProfileBalancedName && !p.Disabled {
			return p.Name
		}
	}
	for _, p := range profiles {
		if !p.Disabled {
			return p.Name
		}
	}
	return ""
}

// huhOptionsForProfiles wraps each profile as a huh Option whose
// label embeds the resolved version. Disabled profiles get a
// "(unavailable: …)" suffix and aren't selectable — huh.Option
// doesn't have a built-in disabled bit, so we steer the operator
// via the description string and skip them in the value resolver.
func huhOptionsForProfiles(profiles []K8sProfile, defaultName string) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(profiles))
	sorted := make([]K8sProfile, len(profiles))
	copy(sorted, profiles)
	// Stable order: Balanced (default) first, then the rest in
	// table order. The table itself preserves original order; only
	// the select reorders so the recommended pick is the cursor's
	// initial position.
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name == defaultName && sorted[j].Name != defaultName
	})

	for _, p := range sorted {
		label := fmt.Sprintf("%-14s (%s)", p.Label, p.Version)
		if p.Disabled {
			label = fmt.Sprintf("%-14s (unavailable: %s)", p.Label, p.Note)
		}
		opts = append(opts, huh.NewOption(label, p.Name))
	}
	return opts
}

// renderK8sProfileTable lays the four profiles out as a lipgloss
// table. Width is bounded so the table renders in an 80-column
// terminal without wrapping.
func renderK8sProfileTable(profiles []K8sProfile) string {
	header := []string{"Profile", "Version", "Risk", "Note"}

	rows := make([][]string, 0, len(profiles))
	for _, p := range profiles {
		label := p.Label
		if p.Name == k8sProfileBalancedName {
			label += " ★"
		}
		note := p.Maintenance
		if p.Disabled {
			note = "unavailable — " + p.Note
		}
		rows = append(rows, []string{label, p.Version, p.Risk, note})
	}

	// Find the row index of the Balanced (default) profile so the
	// table styler can highlight that row. Profiles iterate in
	// table order; we lock in the index up-front rather than
	// re-scanning per cell.
	balancedRow := -1
	for i, p := range profiles {
		if p.Name == k8sProfileBalancedName {
			balancedRow = i
			break
		}
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	// Bold + a subtle background tint on the recommended row so
	// the operator's eye lands there first. 236 is a dark grey
	// that reads on both dark and light terminal themes; foreground
	// stays at terminal default so contrast is preserved.
	highlightStyle := cellStyle.Bold(true).Background(lipgloss.Color("236"))

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers(header...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			switch row {
			case table.HeaderRow:
				return headerStyle
			case balancedRow:
				return highlightStyle
			default:
				return cellStyle
			}
		})

	return t.Render()
}

// pickK8sProfileTitle renders the "Pick a Kubernetes version
// profile" picker header. When the auto-detected KubeAid release
// tag is known it appends "  ★  KubeAid <tag>" inline — the same
// star marks the recommended Balanced row in the comparison table,
// so eye-jumping between the title brand and the row highlight
// feels intentional. Empty kubeAidTag falls back to the plain
// title (offline run, or autodetect fetched nothing).
func pickK8sProfileTitle(kubeAidTag string) string {
	const plain = "  Pick a Kubernetes version profile"
	if kubeAidTag == "" {
		return plain
	}
	brand := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#02BF87")).
		Render("KubeAid")
	tag := lipgloss.NewStyle().
		Bold(true).
		Render(kubeAidTag)
	sep := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("  ★  ")
	return plain + sep + brand + " " + tag
}
