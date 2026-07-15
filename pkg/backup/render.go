// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package backup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// cellHealth selects how a table cell is coloured.
type cellHealth int

const (
	// cellNeutral is the default (no health meaning) style.
	cellNeutral cellHealth = iota
	cellOK
	cellWarn
	cellUnknown
)

const (
	// placeholderMissing renders an absent metric series.
	placeholderMissing = "—"

	colorGreen = "42"
	colorRed   = "203"

	// Column headers shared across the PostgreSQL and Velero tables.
	headerNamespace = "NAMESPACE"
	headerStatus    = "STATUS"
)

// Render lays the report out as coloured tables with a trailing footer.
// wide expands the PostgreSQL age/interval columns.
func (r *Report) Render(wide bool) string {
	var b strings.Builder

	wrote := false
	if len(r.Postgres) > 0 {
		b.WriteString(sectionTitle("PostgreSQL backups") + "\n")
		b.WriteString(r.renderPostgres(wide) + "\n")
		wrote = true
	}
	if len(r.Velero) > 0 {
		if wrote {
			b.WriteString("\n")
		}
		b.WriteString(sectionTitle("Velero backups") + "\n")
		b.WriteString(r.renderVelero() + "\n")
		wrote = true
	}
	if !wrote {
		b.WriteString("No backup metrics reported.\n")
	}

	if len(r.VeleroErrors) > 0 {
		b.WriteString("\n" + veleroErrorNote(r.VeleroErrors) + "\n")
	}

	b.WriteString("\n" + footer(r.GeneratedAt) + "\n")
	return b.String()
}

// renderPostgres renders the PostgreSQL table. The narrow view shows a
// glyph plus the humanized latest age per method; the wide view breaks
// LAST/OLDEST/MAX GAP into their own columns.
func (r *Report) renderPostgres(wide bool) string {
	headers, rows := r.postgresMatrix(wide)

	statusCol := len(headers) - 1
	return renderColoredTable(headers, rows, func(rowIdx, colIdx int) cellHealth {
		row := r.Postgres[rowIdx]
		switch {
		case !wide && colIdx == 2:
			return toCellHealth(row.Logical.Status)
		case !wide && colIdx == 3:
			return toCellHealth(row.WAL.Status)
		case colIdx == statusCol:
			return toCellHealth(row.Overall())
		default:
			return cellNeutral
		}
	})
}

// renderVelero renders the Velero table with its STATUS column coloured.
func (r *Report) renderVelero() string {
	headers, rows := r.veleroMatrix()

	statusCol := len(headers) - 1
	return renderColoredTable(headers, rows, func(rowIdx, colIdx int) cellHealth {
		if colIdx == statusCol {
			return toCellHealth(r.Velero[rowIdx].Status)
		}
		return cellNeutral
	})
}

// postgresMatrix returns the PostgreSQL headers and string rows.
func (r *Report) postgresMatrix(wide bool) ([]string, [][]string) {
	if wide {
		headers := []string{
			headerNamespace, "CLUSTER",
			"LOGICAL LAST", "LOGICAL OLDEST", "LOGICAL MAX GAP",
			"WAL LAST", "WAL OLDEST", "WAL MAX GAP",
			headerStatus,
		}
		rows := make([][]string, 0, len(r.Postgres))
		for _, row := range r.Postgres {
			overall := row.Overall()
			rows = append(rows, []string{
				row.Namespace, row.ClusterName,
				humanizeAge(row.Logical.Last), humanizeAge(row.Logical.Oldest), humanizeAge(row.Logical.MaxGap),
				humanizeAge(row.WAL.Last), humanizeAge(row.WAL.Oldest), humanizeAge(row.WAL.MaxGap),
				statusText(overall),
			})
		}
		return headers, rows
	}

	headers := []string{headerNamespace, "CLUSTER", "LOGICAL", "WAL", headerStatus}
	rows := make([][]string, 0, len(r.Postgres))
	for _, row := range r.Postgres {
		overall := row.Overall()
		rows = append(rows, []string{
			row.Namespace, row.ClusterName,
			methodCell(row.Logical.Status, row.Logical.Last),
			methodCell(row.WAL.Status, row.WAL.Last),
			statusGlyph(overall) + " " + statusText(overall),
		})
	}
	return headers, rows
}

// veleroMatrix returns the Velero headers and string rows.
func (r *Report) veleroMatrix() ([]string, [][]string) {
	headers := []string{headerNamespace, "RESOURCE", "TYPE", "METHOD", "LAST", "OLDEST", "MAX GAP", headerStatus}
	rows := make([][]string, 0, len(r.Velero))
	for _, row := range r.Velero {
		rows = append(rows, []string{
			row.Namespace, row.ResourceName, row.ResourceType, row.Method,
			humanizeAge(row.Last), humanizeAge(row.Oldest), humanizeAge(row.MaxGap),
			statusGlyph(row.Status) + " " + statusText(row.Status),
		})
	}
	return headers, rows
}

// methodCell is a narrow-view PostgreSQL cell: a health glyph plus the
// humanized latest-backup age.
func methodCell(status healthState, last ageCell) string {
	return statusGlyph(status) + " " + humanizeAge(last)
}

// renderColoredTable renders a rounded-border table, colouring each body
// cell by the cellHealth reported by healthOf.
func renderColoredTable(headers []string, rows [][]string, healthOf func(row, col int) cellHealth) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	okStyle := cellStyle.Foreground(lipgloss.Color(colorGreen))
	warnStyle := cellStyle.Foreground(lipgloss.Color(colorRed))
	unknownStyle := cellStyle.Faint(true)

	return table.New().
		Border(lipgloss.RoundedBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch healthOf(row, col) {
			case cellOK:
				return okStyle
			case cellWarn:
				return warnStyle
			case cellUnknown:
				return unknownStyle
			case cellNeutral:
				return cellStyle
			default:
				return cellStyle
			}
		}).
		Render()
}

// humanizeAge renders an ageCell as a compact relative age, or the
// missing-series placeholder when absent.
func humanizeAge(c ageCell) string {
	if !c.Present {
		return placeholderMissing
	}
	return humanizeSeconds(c.Seconds)
}

// humanizeSeconds renders an age in seconds as the most significant
// non-zero unit plus the next-smaller unit when that is non-zero (up to
// two adjacent units), e.g. "1h 1m ago", "3d 4h ago", "42s ago".
func humanizeSeconds(total float64) string {
	if total < 0 {
		total = 0
	}
	secs := int64(total)

	units := []struct {
		value  int64
		suffix string
	}{
		{secs / 86400, "d"},
		{(secs % 86400) / 3600, "h"},
		{(secs % 3600) / 60, "m"},
		{secs % 60, "s"},
	}

	first := -1
	for i, u := range units {
		if u.value > 0 {
			first = i
			break
		}
	}
	if first == -1 {
		return "0s ago"
	}

	parts := []string{fmt.Sprintf("%d%s", units[first].value, units[first].suffix)}
	if first+1 < len(units) && units[first+1].value > 0 {
		parts = append(parts, fmt.Sprintf("%d%s", units[first+1].value, units[first+1].suffix))
	}
	return strings.Join(parts, " ") + " ago"
}

// statusText is the STATUS column word for a healthState.
func statusText(h healthState) string {
	switch h {
	case healthOK:
		return "OK"
	case healthDegraded:
		return "DEGRADED"
	case healthUnknown:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// statusGlyph is the leading glyph for a healthState.
func statusGlyph(h healthState) string {
	switch h {
	case healthOK:
		return "✓"
	case healthDegraded:
		return "✗"
	case healthUnknown:
		return "?"
	default:
		return "?"
	}
}

// toCellHealth maps a derived healthState onto a cell colour.
func toCellHealth(h healthState) cellHealth {
	switch h {
	case healthOK:
		return cellOK
	case healthDegraded:
		return cellWarn
	case healthUnknown:
		return cellUnknown
	default:
		return cellUnknown
	}
}

func sectionTitle(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}

func footer(generatedAt string) string {
	return lipgloss.NewStyle().Faint(true).Render("data as of " + generatedAt)
}

func veleroErrorNote(types []string) string {
	style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorRed))
	return style.Render("⚠ Velero exporter reported errors for type(s): " + strings.Join(types, ", "))
}
