// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPSURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		errParts []string
	}{
		{
			name:    "valid https URL with path passes",
			input:   "https://keycloak.example/realms/clusters",
			wantErr: false,
		},
		{
			name:    "valid https URL with port passes",
			input:   "https://keycloak.example:8443/realms/x",
			wantErr: false,
		},
		{
			name:     "empty string is required",
			input:    "",
			wantErr:  true,
			errParts: []string{"required"},
		},
		{
			name:     "http (not https) is rejected",
			input:    "http://keycloak.example/realms/x",
			wantErr:  true,
			errParts: []string{"https"},
		},
		{
			name:     "no scheme is rejected",
			input:    "keycloak.example/realms/x",
			wantErr:  true,
			errParts: []string{"https"},
		},
		{
			name:     "https without host is rejected",
			input:    "https:///realms/x",
			wantErr:  true,
			errParts: []string{"host"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := httpsURL(tc.input)

			if !tc.wantErr {
				assert.NoError(t, err)
				return
			}

			require.Error(t, err)

			for _, part := range tc.errParts {
				assert.Contains(t, err.Error(), part,
					"want error to mention %q, got %q", part, err.Error())
			}
		})
	}
}

func TestDeriveRealmFromDNS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "single-segment TLD", host: "keycloak.vpn.acme.com", want: "acme"},
		{name: "multi-part TLD .co.uk", host: "kc.foo.co.uk", want: "foo"},
		{name: "two-segment domain", host: "kc.acme.com", want: "acme"},
		{name: "bare apex", host: "acme.com", want: "acme"},
		{name: "empty input", host: "", want: ""},
		{name: "whitespace-only input", host: "  \t ", want: ""},
		{name: "host with no public suffix", host: "localhost", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, deriveRealmFromDNS(tc.host))
		})
	}
}

func TestStripFirstLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "standard keycloak prefix", host: "keycloak.vpn.acme.com", want: "vpn.acme.com"},
		{name: "single-label first", host: "auth.acme.com", want: "acme.com"},
		{name: "two-segment apex", host: "acme.com", want: "com"},
		{name: "single-label host has no first label to strip", host: "localhost", want: ""},
		{name: "empty input", host: "", want: ""},
		{name: "whitespace gets trimmed before stripping", host: "  keycloak.vpn.acme.com  ", want: "vpn.acme.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, stripFirstLabel(tc.host))
		})
	}
}

func TestDeriveACMEEmailFromDNS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "two-segment domain → ops@apex", host: "vpn.obmondo.com", want: "ops@obmondo.com"},
		{name: "deeply-nested domain → ops@eTLD+1", host: "k8s.dev.team.acme.com", want: "ops@acme.com"},
		{name: "multi-part TLD", host: "vpn.foo.co.uk", want: "ops@foo.co.uk"},
		{name: "empty input", host: "", want: ""},
		{name: "host without a public suffix", host: "localhost", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, deriveACMEEmailFromDNS(tc.host))
		})
	}
}

func TestNonEmpty(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "non-empty string passes", input: "hello", wantErr: false},
		{name: "empty string is required", input: "", wantErr: true},
		{name: "whitespace-only is required", input: "   \t  ", wantErr: true},
		{name: "newline-only is required", input: "\n", wantErr: true},
		{name: "string with surrounding whitespace passes", input: "  ok  "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := nonEmpty(tc.input)
			if tc.wantErr {
				assert.True(
					t,
					errors.Is(err, errRequired),
					"expected errRequired sentinel, got %v",
					err,
				)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestWriteTemplatedFile(t *testing.T) {
	tests := []struct {
		name        string
		setupDest   func(t *testing.T) string
		template    string
		data        any
		perm        os.FileMode
		wantContent string
		wantPerm    os.FileMode
		wantErr     bool
		wantErrSub  string
	}{
		{
			name: "writes rendered template to a fresh nested path",
			setupDest: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "nested", "general.yaml")
			},
			template:    "region: {{ .Region }}\nreplicas: {{ .Replicas }}\n",
			data:        struct{ Region, Replicas string }{Region: "eu-west-1", Replicas: "3"},
			perm:        0o600,
			wantContent: "region: eu-west-1\nreplicas: 3\n",
			wantPerm:    0o600,
		},
		{
			name: "overwrites existing file content via O_TRUNC",
			setupDest: func(t *testing.T) string {
				dir := t.TempDir()
				out := filepath.Join(dir, "out")
				oldContent := []byte("old content way longer than the new one")
				require.NoError(t, os.WriteFile(out, oldContent, 0o600))
				return out
			},
			template:    "{{ . }}",
			data:        "new",
			perm:        0o600,
			wantContent: "new",
			wantPerm:    0o600,
		},
		{
			name: "perm parameter is plumbed through to a fresh file",
			setupDest: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "fresh-out")
			},
			template:    "{{ . }}",
			data:        "x",
			perm:        0o644,
			wantContent: "x",
			wantPerm:    0o644,
		},
		{
			name:       "invalid template syntax surfaces as error",
			setupDest:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "out") },
			template:   "{{ .Unclosed",
			data:       "x",
			perm:       0o600,
			wantErr:    true,
			wantErrSub: "parsing template",
		},
		{
			name:       "template execution error is reported",
			setupDest:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "out") },
			template:   "{{ .Missing }}",
			data:       42,
			perm:       0o600,
			wantErr:    true,
			wantErrSub: "rendering template",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dest := tc.setupDest(t)

			err := writeTemplatedFile(dest, tc.template, tc.data, tc.perm)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSub)
				return
			}
			require.NoError(t, err)

			got, err := os.ReadFile(dest)
			require.NoError(t, err)
			assert.Equal(t, tc.wantContent, string(got))

			info, err := os.Stat(dest)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPerm, info.Mode().Perm())
		})
	}
}
