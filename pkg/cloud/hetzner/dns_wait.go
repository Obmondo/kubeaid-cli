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
	// \r-overwrite our per-FQDN status lines (the spinner anchors at
	// col 0 of the cursor's current line; without pausing, ticks
	// scribble "⠋ [16s]" prefixes onto our output). Resume on exit so
	// the next bootstrap step picks up a live spinner.
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

	printDNSWaitHeader(fqdns, expectedIP)

	// Save cursor at the start of the per-tick status block. Each
	// iteration restores cursor + clears to end of screen, so the
	// rewrite happens in place — the operator sees one stable status
	// table that updates, not a wall of accumulating per-tick lines.
	fmt.Print("\033[s")

	for {
		fmt.Print("\033[u\033[J")
		ok := checkAllResolve(ctx, resolver, fqdns, expectedIP)
		if ok {
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

func printDNSWaitHeader(fqdns []string, expectedIP string) {
	fmt.Println()
	fmt.Println("Add the following A records to your DNS, then this will continue automatically:")
	for _, fqdn := range fqdns {
		fmt.Printf("  %-40s A   %s\n", fqdn, expectedIP)
	}
	fmt.Println()
	fmt.Printf("Polling every %s; Ctrl+C to abort, 's' + Enter to skip.\n", dnsPollInterval)
	fmt.Println()
}

// checkAllResolve queries every fqdn and prints its current
// resolution. Returns true when every fqdn resolves to expectedIP.
// One miss (NXDOMAIN, wrong IP, query error) is enough to return
// false — the loop will retry on the next tick.
func checkAllResolve(ctx context.Context, resolver *net.Resolver, fqdns []string, expectedIP string) bool {
	allOK := true
	for _, fqdn := range fqdns {
		got, err := lookupA(ctx, resolver, fqdn)
		switch {
		case err != nil:
			fmt.Printf("  %-40s %s\n", fqdn, "lookup failed: "+err.Error())
			allOK = false
		case got == "":
			fmt.Printf("  %-40s NXDOMAIN\n", fqdn)
			allOK = false
		case got != expectedIP:
			fmt.Printf("  %-40s %s (expected %s)\n", fqdn, got, expectedIP)
			allOK = false
		default:
			fmt.Printf("  %-40s %s ✓\n", fqdn, got)
		}
	}
	fmt.Println()
	return allOK
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
