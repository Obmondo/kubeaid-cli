// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package giturl

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// ParsedURL is the structured representation of a Git remote URL.
// Host-agnostic — works with any forge (github.com, gitea.example.com:2223,
// self-hosted gitlab, etc.). Use Parse to construct.
type ParsedURL struct {
	// Host is the network authority — "github.com" or
	// "gitea.example.com:2223". Includes port when non-standard.
	Host string

	// Owner is the first path segment — typically the org or user.
	Owner string

	// Repo is the second path segment, with any trailing ".git"
	// stripped.
	Repo string

	// Scheme is the URL scheme — "https", "http", or "ssh"
	// (scp-style git@host:path is reported as "ssh").
	Scheme string
}

// scpSyntax matches the scp-style Git URL form `[user@]host:path`.
// Per git-clone(1) "Git URLs": treated identically to ssh://. The
// user part is optional (defaults to "git" in practice); host and
// path use the conservative character classes git itself accepts.
var scpSyntax = regexp.MustCompile(`^(?:[a-zA-Z0-9._-]+@)?([a-zA-Z0-9.-]+):([a-zA-Z0-9./_-]+)$`)

// Parse extracts host/owner/repo/scheme from a Git remote URL.
// Accepted forms:
//
//   - https://host[:port]/owner/repo[.git]
//   - http://host[:port]/owner/repo[.git]
//   - ssh://[user@]host[:port]/owner/repo[.git]
//   - [user@]host:owner/repo[.git]   (scp-style)
//
// Returns an error on empty input, unparseable URLs, or paths that
// don't carry at least owner/repo segments.
func Parse(s string) (*ParsedURL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("git URL is empty")
	}

	// scheme://... → handled by stdlib url.Parse.
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("parsing git URL %q: %w", s, err)
		}
		owner, repo, err := splitOwnerRepo(u.Path)
		if err != nil {
			return nil, fmt.Errorf("git URL %q: %w", s, err)
		}
		return &ParsedURL{
			Host:   u.Host,
			Owner:  owner,
			Repo:   repo,
			Scheme: u.Scheme,
		}, nil
	}

	// scp-style: [user@]host:path. url.Parse can't parse this — it
	// reads "git@host:path" as scheme="git@host" / opaque="path",
	// so we match the form explicitly.
	if m := scpSyntax.FindStringSubmatch(s); m != nil {
		owner, repo, err := splitOwnerRepo(m[2])
		if err != nil {
			return nil, fmt.Errorf("git URL %q: %w", s, err)
		}
		return &ParsedURL{
			Host:   m[1],
			Owner:  owner,
			Repo:   repo,
			Scheme: "ssh",
		}, nil
	}

	return nil, fmt.Errorf("unsupported git URL form: %q", s)
}

// HTTPCloneURL returns the HTTPS clone-URL form regardless of the
// original scheme — useful when constructing forge URLs for users
// to click (e.g., GitHub's "/compare/..." page) without leaking
// agent-routed credentials.
func (p *ParsedURL) HTTPCloneURL() string {
	return fmt.Sprintf("https://%s/%s/%s.git", p.Host, p.Owner, p.Repo)
}

// HostName returns Host with any "<host>:<port>" suffix stripped.
// Use for filesystem paths (where the colon would break tools like
// docker's -v <src>:<dst> volume specs) and TLS SAN comparisons
// (where the port is irrelevant). Falls back to Host unchanged
// when there's no port to strip.
func (p *ParsedURL) HostName() string {
	if h, _, err := net.SplitHostPort(p.Host); err == nil {
		return h
	}
	return p.Host
}

// splitOwnerRepo extracts the first two non-empty segments from a
// URL path. Anything past owner/repo is ignored (gitea/gitlab
// nested groups would land here; we treat them as out of scope and
// pick the leading two).
func splitOwnerRepo(path string) (owner, repo string, err error) {
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("path %q must be owner/repo[.git]", path)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}
