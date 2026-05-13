// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package giturl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHTTP(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "https URL", url: "https://github.com/org/repo.git", want: true},
		{name: "http URL with port", url: "http://gitea.local:3000/org/repo.git", want: true},
		{name: "ssh URL", url: "ssh://git@github.com/org/repo.git", want: false},
		{name: "scp-style git URL", url: "git@github.com:org/repo.git", want: false},
		{name: "empty string", url: "", want: false},
		{name: "uppercase HTTPS not matched", url: "HTTPS://github.com/org/repo.git", want: false},
		{name: "leading whitespace not stripped", url: " https://github.com/org/repo.git", want: false},
		{name: "scheme-like prefix without colon-slashes", url: "httpsfoo://example", want: false},
		{name: "http inside path is not a prefix match", url: "git+http://example", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsHTTP(tc.url))
		})
	}
}

func TestIsSSH(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "scp-style git URL", url: "git@github.com:org/repo.git", want: true},
		{name: "ssh:// URL", url: "ssh://git@github.com/org/repo.git", want: true},
		{name: "https URL", url: "https://github.com/org/repo.git", want: false},
		{name: "http URL", url: "http://gitea.local:3000/org/repo.git", want: false},
		{name: "empty string", url: "", want: false},
		// IsSSH is a "not HTTP and not empty" predicate, so this lands as
		// SSH; the prompt-side validator catches genuinely-malformed input.
		{name: "non-URL garbage classified as SSH", url: "totally-bogus", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsSSH(tc.url))
		})
	}
}
