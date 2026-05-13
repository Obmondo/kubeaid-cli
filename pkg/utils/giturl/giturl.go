// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package giturl is a tiny, dependency-free predicate package for
// classifying Git remote URL forms (HTTP(s) vs SSH). Lives outside
// pkg/utils/git so callers in pkg/config and pkg/config/prompt can
// import it without the import cycle that pkg/utils/git creates by
// depending on pkg/config.
package giturl

import "strings"

// IsHTTP reports whether s is an HTTP-form Git URL — i.e. starts
// with http:// or https://. Trailing whitespace is not trimmed;
// callers that accept user input should TrimSpace upstream.
func IsHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// IsSSH reports whether s looks like an SSH-form Git URL — i.e. is
// non-empty and not HTTP. Both scp-style (git@host:path) and rfc-3986
// (ssh://user@host/path) forms qualify.
func IsSSH(s string) bool {
	return s != "" && !IsHTTP(s)
}
