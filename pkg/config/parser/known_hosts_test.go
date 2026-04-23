// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"strings"
	"testing"
)

func TestValidateKnownHostsEntries(t *testing.T) {
	// A real ed25519 public key. ssh.ParseKnownHosts validates the key
	// material itself (not just the format), so we need a cryptographically
	// valid key; the host strings can be whatever.
	validKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHZLLpBn+ig1bdyf+9SLB0wbIMcfaNs+M+Co7ZW0ykzl"
	validPlainHost := "gitea.example.com " + validKey
	validPortHost := "[gitea.example.com]:2223 " + validKey

	tests := []struct {
		name       string
		entries    []string
		wantErr    bool
		wantErrMsg string // substring the error message must contain
	}{
		{
			name:    "nil slice passes",
			entries: nil,
		},
		{
			name:    "empty slice passes",
			entries: []string{},
		},
		{
			name:    "single valid hostname entry passes",
			entries: []string{validPlainHost},
		},
		{
			name:    "valid entry with bracketed host:port passes",
			entries: []string{validPortHost},
		},
		{
			name:    "mix of valid entries passes",
			entries: []string{validPlainHost, validPortHost},
		},
		{
			name:       "empty entry is rejected",
			entries:    []string{""},
			wantErr:    true,
			wantErrMsg: "entry 0 is empty",
		},
		{
			name:       "whitespace-only entry is rejected",
			entries:    []string{"   \t  "},
			wantErr:    true,
			wantErrMsg: "entry 0 is empty",
		},
		{
			name:       "malformed entry is rejected",
			entries:    []string{"this is not a known_hosts line"},
			wantErr:    true,
			wantErrMsg: "entry 0",
		},
		{
			name:       "multi-line entry is rejected",
			entries:    []string{validPlainHost + "\n" + validPortHost},
			wantErr:    true,
			wantErrMsg: "contains multiple lines",
		},
		{
			name:       "second entry invalid reports correct index",
			entries:    []string{validPlainHost, "garbage"},
			wantErr:    true,
			wantErrMsg: "entry 1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateKnownHostsEntries(context.Background(), tc.entries)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}
