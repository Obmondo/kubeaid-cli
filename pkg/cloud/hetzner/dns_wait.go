// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

// dnsResolveTimeout caps how long a single resolver query can take —
// keeps the per-tick latency bounded when DNS is unreachable. Each
// fqdn is queried sequentially through each configured resolver, so
// the worst-case tick takes len(fqdns) * len(resolvers) *
// dnsResolveTimeout.
const dnsResolveTimeout = 3 * time.Second

// dnsPollInterval is how long to wait between rechecking the FQDNs
// when at least one didn't resolve to the expected IP. 10s gives
// the operator time to actually go to their DNS provider, paste the
// records, and come back without us hammering the resolver in the
// meantime.
const dnsPollInterval = 10 * time.Second

// dnsTotalTimeout caps the entire wait. Ten minutes is enough for
// most fast-propagating providers (Cloudflare, Route53, Hetzner DNS)
// plus a generous buffer for the operator to navigate to their DNS
// provider, paste several records, and have those propagate through
// the local resolver. Past that, we abort the bootstrap with a clear
// error instead of looping forever — the operator's session is
// interactive (YubiKey touches), so blocking indefinitely on a
// missing record is worse than failing fast.
const dnsTotalTimeout = 10 * time.Minute

// WaitForDNSResolution blocks until every fqdn in fqdns resolves to
// expectedIP through the OS resolver, ctx is cancelled, or
// dnsTotalTimeout passes (the wait fails closed — interactive bootstrap
// can't loop forever on a missing record).
//
// No skip option: bypassing the wait would just push the failure into
// a later stage (cert-manager's first ACME HTTP-01, NetBird OIDC
// callback, etc.) where the symptom is harder to diagnose. Better to
// fail here with a clear "DNS records still not resolving" + cache-
// flush hint than to half-bootstrap and debug from a downstream pod's
// 503.
func WaitForDNSResolution(ctx context.Context, fqdns []string, expectedIP string) error {
	if len(fqdns) == 0 || expectedIP == "" {
		return nil
	}

	// Pause the bar's spinner so its 100ms auto-render goroutine can't
	// \r-overwrite our table rows (the spinner anchors at col 0 of the
	// cursor's current line; without pausing, ticks scribble "⠋ [16s]"
	// across the table). Resume on exit so the next bootstrap step
	// picks up a live spinner.
	bar := progress.FromCtx(ctx)
	bar.Pause()
	defer bar.Resume()

	// Use the operator's OS resolver. If they have a stale NXDOMAIN
	// cached for one of our records, that's a local-cache problem
	// they need to flush themselves — adding a public-resolver
	// fallback caused false-negative i/o timeouts on networks that
	// block egress to 185.12.64.1:53 (corporate firewall, restrictive
	// VPN), and the symptom (rows stuck on "✗ i/o timeout" while
	// `nslookup` works fine on the same machine) was confusing.
	resolver := net.DefaultResolver

	fmt.Println()
	fmt.Println("Add the A records shown below to your DNS provider — bootstrap continues automatically once they all resolve.")
	fmt.Println()

	maxAttempts := int(dnsTotalTimeout / dnsPollInterval)
	start := time.Now()
	deadline := start.Add(dnsTotalTimeout)

	// Track the last-rendered block so we can re-measure its visible
	// height (which depends on the *current* terminal width) and wipe
	// exactly that many rows on the next tick. `\033[s` / `\033[u`
	// cursor save/restore drifts once the saved position scrolls past
	// the viewport (each tick then leaks into scrollback); cursor-up
	// + clear-to-end is deterministic, but the up-count has to
	// account for wrapping (operator splits a tmux pane mid-run →
	// narrower width → single-line rows wrap to two) — see
	// progress.RenderedLineCount.
	//
	// Render an initial "querying…" table immediately so the operator
	// gets feedback up front. Without it, the first round of lookups
	// (up to len(fqdns) * dnsResolveTimeout when records are missing)
	// leaves the screen looking frozen with just the header text.
	pending := make([]dnsStatus, len(fqdns))
	for i, fqdn := range fqdns {
		pending[i] = dnsStatus{FQDN: fqdn, Pending: true}
	}
	initialBlock := buildDNSWaitBlock(1, maxAttempts, time.Since(start), pending, expectedIP)
	fmt.Print(initialBlock)
	prevBlock := initialBlock

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		statuses := resolveAll(ctx, resolver, fqdns, expectedIP)

		block := buildDNSWaitBlock(attempt, maxAttempts, time.Since(start), statuses, expectedIP)
		if prevBlock != "" {
			fmt.Printf("\033[%dF\033[J", progress.RenderedLineCount(prevBlock))
		}
		fmt.Print(block)
		prevBlock = block

		if allDNSStatusesOK(statuses) {
			// No "DNS verified" trailer — the all-green status column
			// in the rendered table is the confirmation. Adding a
			// "continuing" line below would be redundant noise.
			return nil
		}

		// Bail out cleanly when the deadline is reached after the
		// last attempt — operator's session is interactive (YubiKey),
		// so a clear timeout error beats blocking forever. Print the
		// stale-cache hint to stderr first; if the operator sees the
		// records in their DNS provider but our resolver still says
		// NXDOMAIN, a stale local cache is the usual cause.
		if time.Now().After(deadline) || attempt == maxAttempts {
			printStaleCacheHint(os.Stderr, expectedIP)
			return fmt.Errorf("DNS verification timed out after %s", dnsTotalTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dnsPollInterval):
		}
	}
	// Unreachable — the loop returns from inside the deadline check.
	// The compiler can't see that, so spell out the same error.
	return fmt.Errorf("DNS verification timed out after %s", dnsTotalTimeout)
}

// printStaleCacheHint writes a short troubleshooting note to w when
// the wait times out. Two paths cover ~all operator setups:
//
//	Linux + systemd-resolved (Ubuntu/Debian/Fedora/Arch defaults)
//	macOS (mDNSResponder + dscacheutil)
//
// We don't try to enumerate every possible setup (nscd, dnsmasq,
// Unbound, BIND-on-laptop) — those operators know their own resolver
// and don't need the hint. The two listed cover the muscle-memory
// case of a freshly-installed records hitting a stale local cache.
func printStaleCacheHint(w io.Writer, expectedIP string) {
	_, _ = fmt.Fprintf(w, "\n"+
		"DNS records still not resolving to %s.\n\n"+
		"Possible causes:\n"+
		"  1. The records aren't yet propagated, or are typo'd in your DNS provider.\n"+
		"  2. Your local resolver has a stale NXDOMAIN cached. Flush it:\n"+
		"       Linux (systemd-resolved):  sudo resolvectl flush-caches\n"+
		"       macOS:                     sudo dscacheutil -flushcache && sudo killall -HUP mDNSResponder\n"+
		"\n"+
		"Fix DNS, then re-run `kubeaid-cli cluster bootstrap`.\n"+
		"\n", expectedIP)
}

// buildDNSWaitBlock returns the per-tick render — the header line
// (attempt + elapsed + remaining + Ctrl+C hint) plus the lipgloss
// status table — as a single string ending in a newline. Returning a
// string (not printing directly) lets the caller count lines for the
// cursor-up wipe on the next tick.
//
// Ctrl+C rides on the same line as the timer (top of the redraw block)
// rather than as a separate footer; the bottom-footer placement
// duplicated visual weight with the table border below it.
func buildDNSWaitBlock(attempt, maxAttempts int, elapsed time.Duration, statuses []dnsStatus, expectedIP string) string {
	remaining := dnsTotalTimeout - elapsed
	if remaining < 0 {
		remaining = 0
	}
	header := fmt.Sprintf("Attempt %d/%d  •  %s elapsed  •  %s remaining  •  Ctrl+C to abort\n",
		attempt, maxAttempts,
		elapsed.Round(time.Second), remaining.Round(time.Second),
	)
	return header + renderDNSStatusTable(statuses, expectedIP) + "\n"
}

// dnsStatus is one row of the resolution table — what we asked for vs.
// what came back. Used both to render the lipgloss table and to decide
// whether to keep polling.
type dnsStatus struct {
	FQDN string

	// Pending marks the row as not-yet-queried (only true for the
	// initial pre-lookup render). Renders as "querying…" with the
	// neutral cell color, so the operator doesn't see a wall of red
	// "✗ NXDOMAIN" until we've actually checked.
	Pending bool

	Got string // resolved IP; "" when NXDOMAIN
	Err error  // non-nil on transport / resolver errors
	OK  bool   // Got matches the expected IP exactly
}

// resolveAll queries every fqdn through resolver and returns one
// dnsStatus per fqdn. Pure (besides DNS I/O) — separated from rendering
// so the table renderer can be unit-tested without network.
func resolveAll(ctx context.Context, resolver *net.Resolver, fqdns []string, expectedIP string) []dnsStatus {
	out := make([]dnsStatus, 0, len(fqdns))
	for _, fqdn := range fqdns {
		got, err := lookupA(ctx, resolver, fqdn)
		out = append(out, dnsStatus{
			FQDN: fqdn,
			Got:  got,
			Err:  err,
			OK:   err == nil && got == expectedIP,
		})
	}
	return out
}

func allDNSStatusesOK(statuses []dnsStatus) bool {
	for _, s := range statuses {
		if !s.OK {
			return false
		}
	}
	return true
}

// renderDNSStatusTable lays the dnsStatus rows out as a lipgloss table
// with a rounded border (same visual style as the K8s profile picker).
// The Status column is colored — green when the row's OK, red
// otherwise — so the operator can scan a long table at a glance.
func renderDNSStatusTable(statuses []dnsStatus, expectedIP string) string {
	headers := []string{"FQDN", "Expected A", "Status"}

	rows := make([][]string, 0, len(statuses))
	for _, s := range statuses {
		rows = append(rows, []string{s.FQDN, expectedIP, dnsStatusCell(s, expectedIP)})
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)
	okStyle := cellStyle.Foreground(lipgloss.Color("42"))   // green
	errStyle := cellStyle.Foreground(lipgloss.Color("203")) // red

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if col == 2 { // Status column
				switch {
				case statuses[row].Pending:
					return cellStyle
				case statuses[row].OK:
					return okStyle
				default:
					return errStyle
				}
			}
			return cellStyle
		})

	return t.Render()
}

// dnsStatusCell formats a single Status-column cell. Keeps the
// branching out of renderDNSStatusTable so the rendering loop reads
// linearly.
func dnsStatusCell(s dnsStatus, expectedIP string) string {
	switch {
	case s.Pending:
		return "querying…"
	case s.OK:
		return "✓ " + s.Got
	case s.Err != nil:
		msg := s.Err.Error()
		// Lookup errors on net.DNSError include the resolver lookup
		// path (e.g., "lookup foo.example.com: i/o timeout") which
		// duplicates the FQDN already in the first column. Strip
		// that prefix so the cell stays readable.
		if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "lookup ") {
			msg = msg[i+2:]
		}
		return "✗ " + msg
	case s.Got == "":
		return "✗ NXDOMAIN"
	default:
		return "✗ " + s.Got + " (expected " + expectedIP + ")"
	}
}

// lookupA returns the first IPv4 A record for fqdn through the given
// resolver, "" on NXDOMAIN. Wraps go-stdlib's resolver and unwraps
// the not-found case so callers can distinguish missing from broken.
func lookupA(ctx context.Context, resolver *net.Resolver, fqdn string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsResolveTimeout)
	defer cancel()

	ips, err := resolver.LookupIP(ctx, "ip4", fqdn)
	if err != nil {
		var dnsErr *net.DNSError
		if asDNSErr(err, &dnsErr) && dnsErr.IsNotFound {
			return "", nil
		}
		return "", err
	}
	if len(ips) == 0 {
		return "", nil
	}
	return ips[0].String(), nil
}

// asDNSErr is a small wrapper around errors.As to keep lookupA's
// switch readable. (errors.As wants a **net.DNSError target; the
// extra indirection is awkward inline.)
func asDNSErr(err error, target **net.DNSError) bool {
	for cur := err; cur != nil; {
		if d, ok := cur.(*net.DNSError); ok {
			*target = d
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	return false
}
