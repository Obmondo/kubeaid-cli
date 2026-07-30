// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package url

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
