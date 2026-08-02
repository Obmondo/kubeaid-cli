// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNonEmpty(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "non-empty value passes", in: "acme", wantErr: false},
		{name: "empty string fails", in: "", wantErr: true},
		{name: "whitespace-only fails", in: "   ", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NonEmpty(tc.in)
			if tc.wantErr {
				assertErrorIs(t, err, ErrRequired)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	assert.True(t, errors.Is(err, target), "expected error to be ErrRequired, got %v", err)
}

func TestClusterName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "valid lowercase name", in: "acme-prod", wantErr: false},
		{name: "single character", in: "a", wantErr: false},
		{name: "empty fails", in: "", wantErr: true},
		{name: "dots are rejected", in: "acme.prod", wantErr: true},
		{name: "uppercase fails RFC-1123", in: "Acme", wantErr: true},
		{name: "leading dash fails RFC-1123", in: "-acme", wantErr: true},
		{name: "trailing dash fails RFC-1123", in: "acme-", wantErr: true},
		{name: "underscore fails RFC-1123", in: "acme_prod", wantErr: true},
		{
			name:    "over 63 characters fails",
			in:      "a234567890123456789012345678901234567890123456789012345678901234",
			wantErr: true,
		},
		{
			name:    "exactly 63 characters passes",
			in:      "a2345678901234567890123456789012345678901234567890123456789012",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ClusterName(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSSHGitURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "scp-like form passes", in: "git@github.com:acme/repo.git", wantErr: false},
		{name: "ssh:// form passes", in: "ssh://git@github.com/acme/repo.git", wantErr: false},
		{name: "https is rejected", in: "https://github.com/acme/repo.git", wantErr: true},
		{name: "http is rejected", in: "http://github.com/acme/repo.git", wantErr: true},
		{name: "empty fails", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := SSHGitURL(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestIPv4(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "valid IPv4", in: "10.0.0.5", wantErr: false},
		{name: "empty fails", in: "", wantErr: true},
		{name: "IPv6 fails", in: "::1", wantErr: true},
		{name: "garbage fails", in: "not-an-ip", wantErr: true},
		{name: "out-of-range octet fails", in: "10.0.0.999", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := IPv4(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestCIDRv4(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "valid CIDR", in: "10.0.1.0/24", wantErr: false},
		{name: "empty fails", in: "", wantErr: true},
		{name: "bare IP without prefix fails", in: "10.0.1.0", wantErr: true},
		{name: "garbage fails", in: "not-a-cidr", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CIDRv4(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestHetznerVLANID(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "lower bound passes", in: "4000", wantErr: false},
		{name: "upper bound passes", in: "4091", wantErr: false},
		{name: "mid-range passes", in: "4050", wantErr: false},
		{name: "below range fails", in: "3999", wantErr: true},
		{name: "above range fails", in: "4092", wantErr: true},
		{name: "non-numeric fails", in: "abcd", wantErr: true},
		{name: "empty fails", in: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := HetznerVLANID(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestIPv4InSubnet(t *testing.T) {
	tests := []struct {
		name    string
		cidr    string
		in      string
		wantErr bool
	}{
		{name: "inside subnet passes", cidr: "10.0.1.0/24", in: "10.0.1.5", wantErr: false},
		{name: "outside subnet fails", cidr: "10.0.1.0/24", in: "10.0.2.5", wantErr: true},
		{name: "not an IPv4 fails regardless of subnet", cidr: "10.0.1.0/24", in: "garbage", wantErr: true},
		{name: "empty cidr disables containment check", cidr: "", in: "10.0.2.5", wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := IPv4InSubnet(tc.cidr)(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestHTTPSURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "valid https URL passes", in: "https://issuer.acme.com/oidc", wantErr: false},
		{name: "http is rejected", in: "http://issuer.acme.com/oidc", wantErr: true},
		{name: "empty fails", in: "", wantErr: true},
		{name: "missing host fails", in: "https://", wantErr: true},
		{name: "not a URL fails", in: "://bad", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := HTTPSURL(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
