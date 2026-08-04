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
	"fmt"
	"net"
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

// Parsed is a Git URL broken into its parts.
//
// The type lives here rather than in pkg/repository/url for the same reason
// the protocol constants do: pkg/config embeds it, and that package must
// stay importable without pulling the parsing machinery — which reaches
// oops, samber/lo and golang.org/x/text — along with it. pkg/repository/url
// aliases it and owns Parse.
type Parsed struct {
	Protocol Protocol
	Host,
	Owner,
	Repository string
}

// AsHTTPsURL renders the URL in https form, whatever transport it was
// written in.
func (u *Parsed) AsHTTPsURL() string {
	var hostName string
	switch u.Protocol {
	case ProtocolHTTP, ProtocolHTTPs:
		hostName = u.Host

	case ProtocolSSH, ProtocolSCP:
		hostName = u.HostName()
	}

	return fmt.Sprintf("https://%s/%s/%s.git", hostName, u.Owner, u.Repository)
}

// HostName is Host without any port.
func (u *Parsed) HostName() string {
	if host, _, err := net.SplitHostPort(u.Host); err == nil {
		return host
	}

	return u.Host
}
