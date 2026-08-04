// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package url

import (
	"github.com/Obmondo/kubeaid-cli/pkg/urlprotocol"
)

// The protocol classification lives in pkg/urlprotocol so pkg/render can use
// it without importing this package, whose other file reaches oklog/ulid,
// samber/lo and golang.org/x/text. Re-exported here as aliases rather than
// wrappers, so callers that compare or switch on these values keep working
// against the same types.

type (
	Protocol = urlprotocol.Protocol
	Scheme   = urlprotocol.Scheme
)

const (
	ProtocolHTTP  = urlprotocol.ProtocolHTTP
	ProtocolHTTPs = urlprotocol.ProtocolHTTPs
	ProtocolSSH   = urlprotocol.ProtocolSSH
	ProtocolSCP   = urlprotocol.ProtocolSCP
)

const (
	SchemeHTTP  = urlprotocol.SchemeHTTP
	SchemeHTTPs = urlprotocol.SchemeHTTPs
	SchemeSSH   = urlprotocol.SchemeSSH
	SchemeSCP   = urlprotocol.SchemeSCP
)

func DetectProtocol(unparsedURL string) Protocol {
	return urlprotocol.DetectProtocol(unparsedURL)
}

func UsingHTTPBasedProtocol(unparsedURL string) bool {
	return urlprotocol.UsingHTTPBasedProtocol(unparsedURL)
}

func UsingSSHBasedProtocol(unparsedURL string) bool {
	return urlprotocol.UsingSSHBasedProtocol(unparsedURL)
}
