// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyTransportErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want probeOIDCIssuerErrKind
	}{
		{
			name: "DNS error",
			err:  &net.DNSError{Err: "no such host", Name: "keycloak.nope"},
			want: probeOIDCErrUnresolvable,
		},
		{
			name: "connection refused",
			err:  fmt.Errorf("dial tcp 1.2.3.4:443: connect: connection refused"),
			want: probeOIDCErrConnRefused,
		},
		{
			name: "x509 wrap",
			err:  fmt.Errorf("Get \"https://kc/...\": x509: certificate signed by unknown authority"),
			want: probeOIDCErrTLS,
		},
		{
			name: "tls: handshake failure",
			err:  fmt.Errorf("Get \"https://kc/...\": remote error: tls: handshake failure"),
			want: probeOIDCErrTLS,
		},
		{
			name: "other",
			err:  fmt.Errorf("unexpected EOF"),
			want: probeOIDCErrOther,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, classifyTransportErr(tc.err))
		})
	}
}

func TestRenderProbeOIDCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantSubstrs []string
	}{
		{
			name: "DNS hint mentions NetBird",
			err: &probeOIDCIssuerError{
				Issuer: "https://kc/realms/acme",
				Kind:   probeOIDCErrUnresolvable,
				Cause:  errors.New("no such host"),
			},
			wantSubstrs: []string{"DNS lookup failed", "NetBird VPN"},
		},
		{
			name: "timeout hint mentions mesh path",
			err: &probeOIDCIssuerError{
				Issuer: "https://kc/realms/acme",
				Kind:   probeOIDCErrTimeout,
				Cause:  errors.New("context deadline exceeded"),
			},
			wantSubstrs: []string{"Timed out", "NetBird mesh"},
		},
		{
			name: "HTTP 404 hint mentions realm name",
			err: &probeOIDCIssuerError{
				Issuer: "https://kc/realms/typo",
				Kind:   probeOIDCErrHTTPStatus,
				Status: 404,
				Cause:  errors.New("HTTP 404"),
			},
			wantSubstrs: []string{"HTTP 404", "Realm name"},
		},
		{
			name: "TLS hint mentions caBundlePath",
			err: &probeOIDCIssuerError{
				Issuer: "https://kc/realms/acme",
				Kind:   probeOIDCErrTLS,
				Cause:  errors.New("x509: cert"),
			},
			wantSubstrs: []string{"TLS error", "caBundlePath"},
		},
		{
			name: "issuer mismatch surfaces reported value",
			err: &probeOIDCIssuerError{
				Issuer: "https://kc/realms/acme",
				Kind:   probeOIDCErrIssuerMismatch,
				Got:    "https://kc-internal/realms/acme",
				Cause:  errors.New("mismatch"),
			},
			wantSubstrs: []string{"different issuer", "kc-internal"},
		},
		{
			name:        "non-probe error falls through",
			err:         errors.New("totally unrelated"),
			wantSubstrs: []string{"totally unrelated"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := renderProbeOIDCError(tc.err)
			for _, want := range tc.wantSubstrs {
				assert.Contains(t, got, want)
			}
		})
	}
}

