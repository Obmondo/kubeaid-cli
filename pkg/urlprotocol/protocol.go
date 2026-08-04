// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package urlprotocol classifies a Git URL by the transport it names.
//
// Split out of pkg/repository/url so it can be used from pkg/render, which
// must stay importable without dragging in a dependency graph: the rest of
// pkg/repository/url reaches oklog/ulid, samber/lo and golang.org/x/text,
// none of which a template needs to decide whether a fork URL is SSH-form.
// pkg/repository/url re-exports everything here, so its own callers are
// unaffected.
package urlprotocol

import (
	"strings"
)

const (
	ProtocolHTTP  Protocol = "HTTP"
	ProtocolHTTPs Protocol = "HTTPs"
	ProtocolSSH   Protocol = "SSH"
	ProtocolSCP   Protocol = "SCP"
)

const (
	SchemeHTTP  Scheme = "http://"
	SchemeHTTPs Scheme = "https://"
	SchemeSSH   Scheme = "ssh://"
	SchemeSCP   Scheme = "scp://"
)

type Protocol string

type Scheme string

// DetectProtocol reports which transport unparsedURL names. Anything that
// does not carry an explicit http/https/ssh scheme is treated as SCP, which
// is how "git@host:path" shorthand is classified.
func DetectProtocol(unparsedURL string) Protocol {
	switch {
	case strings.HasPrefix(unparsedURL, string(SchemeHTTP)):
		return ProtocolHTTP

	case strings.HasPrefix(unparsedURL, string(SchemeHTTPs)):
		return ProtocolHTTPs

	case strings.HasPrefix(unparsedURL, string(SchemeSSH)):
		return ProtocolSSH

	default:
		return ProtocolSCP
	}
}

func UsingHTTPBasedProtocol(unparsedURL string) bool {
	protocol := DetectProtocol(unparsedURL)
	return (protocol == ProtocolHTTP) || (protocol == ProtocolHTTPs)
}

func UsingSSHBasedProtocol(unparsedURL string) bool {
	protocol := DetectProtocol(unparsedURL)
	return (protocol == ProtocolSSH) || (protocol == ProtocolSCP)
}
