// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"bufio"
	"context"
	"fmt"
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
// fqdn is queried sequentially, so the worst-case tick takes
// len(fqdns) * dnsResolveTimeout.
const dnsResolveTimeout = 3 * time.Second

// dnsPollInterval is how long to wait between rechecking the FQDNs
// when at least one didn't resolve to the expected IP. 5s strikes a
// balance between operator feedback latency (it's not painful to
// wait) and not hammering the resolver.
const dnsPollInterval = 5 * time.Second

// publicResolverAddr is the recursive resolver kubeaid-cli queries
// instead of the OS's resolver. Hetzner runs their own resolvers; we
// pick theirs so we're not making the operator's first bootstrap
// experience also implicitly endorse a third-party resolver. See
// https://docs.hetzner.com/dns-console/dns/general/recursive-name-servers/.
const publicResolverAddr = "185.12.64.1:53"

// WaitForDNSResolution blocks until every fqdn in fqdns resolves to
// expectedIP through publicResolverAddr, ctx is cancelled, or the
// operator types 's' + Enter on stdin to skip verification.
//
// Prints per-FQDN status on every poll tick so the operator can see
// which records are still propagating. Returns nil when all match
// (the happy path) or when the operator chose to skip; returns
// ctx.Err() if cancelled (Ctrl+C handled by the caller's signal
// handler arrives here as context.Canceled).
//
// Resolver bypass matters: an operator who runs `dig keycloak.X`
// locally may see a stale NXDOMAIN cached by their OS / corporate
// resolver, even though the authoritative zone has the new record.
// Querying a public resolver directly avoids that false negative.
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

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: dnsResolveTimeout}
			return d.DialContext(ctx, network, publicResolverAddr)
		},
	}

	skipCh := make(chan struct{})
	go watchSkipKey(ctx, skipCh)

	fmt.Println()
	fmt.Println("Add the following A records to your DNS, then this will continue automatically.")
	fmt.Printf("Polling every %s; Ctrl+C to abort, 's' + Enter to skip.\n\n", dnsPollInterval)

	// Save cursor at the start of the table block. Each tick restores
	// cursor + clears to end of screen and re-renders, so the operator
	// sees one stable table that updates in place.
	fmt.Print("\033[s")

	for {
		statuses := resolveAll(ctx, resolver, fqdns, expectedIP)

		fmt.Print("\033[u\033[J")
		fmt.Println(renderDNSStatusTable(statuses, expectedIP))

		if allDNSStatusesOK(statuses) {
			fmt.Println("DNS verified, continuing.")
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-skipCh:
			fmt.Println("Skipping DNS verification (operator pressed 's').")
			return nil
		case <-time.After(dnsPollInterval):
		}
	}
}

// dnsStatus is one row of the resolution table — what we asked for vs.
// what came back. Used both to render the lipgloss table and to decide
// whether to keep polling.
type dnsStatus struct {
	FQDN string
	Got  string // resolved IP; "" when NXDOMAIN
	Err  error  // non-nil on transport / resolver errors
	OK   bool   // Got matches the expected IP exactly
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
	okStyle := cellStyle.Foreground(lipgloss.Color("42"))    // green
	errStyle := cellStyle.Foreground(lipgloss.Color("203"))  // red

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if col == 2 { // Status column
				if statuses[row].OK {
					return okStyle
				}
				return errStyle
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

// watchSkipKey closes skipCh when the operator types 's' (case
// insensitive) on stdin. Returns when ctx is cancelled or when stdin
// closes.
//
// We use line-buffered stdin (the default TTY mode) rather than raw
// mode: keeps the implementation small and avoids leaving the
// terminal in a bad state if kubeaid-cli is killed mid-operation.
// The cost is the operator must press Enter after 's', which is
// acceptable for a manual skip operation.
func watchSkipKey(ctx context.Context, skipCh chan<- struct{}) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if strings.EqualFold(strings.TrimSpace(scanner.Text()), "s") {
			close(skipCh)
			return
		}
	}
}
