// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package url

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProtocol(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want Protocol
	}{
		{name: "https URL", url: "https://github.com/org/repo.git", want: ProtocolHTTPs},
		{name: "http URL with port", url: "http://gitea.local:3000/org/repo.git", want: ProtocolHTTP},
		{name: "ssh URL", url: "ssh://git@github.com/org/repo.git", want: ProtocolSSH},
		{name: "scp URL", url: "scp://git@github.com:org/repo.git", want: ProtocolSCP},
		{name: "scp-style git URL", url: "git@github.com:org/repo.git", want: ProtocolSCP},
		// Everything that isn't a recognized scheme prefix falls through
		// to SCP, which is the only scheme-less format. Parse rejects
		// empty input before the protocol matters, and malformed input
		// fails there too.
		{name: "uppercase HTTPS not matched", url: "HTTPS://github.com/org/repo.git", want: ProtocolSCP},
		{name: "leading whitespace not stripped", url: " https://github.com/org/repo.git", want: ProtocolSCP},
		{name: "scheme-like prefix without colon-slashes", url: "httpsfoo://example", want: ProtocolSCP},
		{name: "scheme inside path is not a prefix match", url: "git+http://example", want: ProtocolSCP},
		{name: "empty string", url: "", want: ProtocolSCP},
		{name: "non-URL garbage", url: "totally-bogus", want: ProtocolSCP},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DetectProtocol(tc.url))
		})
	}
}

func TestUsingHTTPBasedProtocol(t *testing.T) {
	assert.True(t, UsingHTTPBasedProtocol("https://github.com/org/repo.git"))
	assert.True(t, UsingHTTPBasedProtocol("http://gitea.local:3000/org/repo.git"))
	assert.False(t, UsingHTTPBasedProtocol("ssh://git@github.com/org/repo.git"))
	assert.False(t, UsingHTTPBasedProtocol("git@github.com:org/repo.git"))
}

func TestUsingSSHBasedProtocol(t *testing.T) {
	assert.True(t, UsingSSHBasedProtocol("ssh://git@github.com/org/repo.git"))
	assert.True(t, UsingSSHBasedProtocol("git@github.com:org/repo.git"))
	assert.False(t, UsingSSHBasedProtocol("https://github.com/org/repo.git"))
	assert.False(t, UsingSSHBasedProtocol("http://gitea.local:3000/org/repo.git"))
}
