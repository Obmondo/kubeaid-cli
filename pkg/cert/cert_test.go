// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package cert_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cert"
)

// writeCert writes a self-signed cert with the given CN to a temp file and
// returns the path.
func writeCert(t *testing.T, cn string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	f, err := os.CreateTemp(t.TempDir(), "cert-*.pem")
	require.NoError(t, err)

	require.NoError(t, pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	require.NoError(t, f.Close())

	return f.Name()
}

func TestReadCN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cn      string
		wantErr bool
	}{
		{
			name: "valid CN",
			cn:   "mycluster.customerabc",
		},
		{
			name: "empty CN is allowed by cert package (split enforces structure)",
			cn:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeCert(t, tc.cn)

			got, err := cert.ReadCN(path)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.cn, got)
		})
	}
}

func TestReadCN_Errors(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()
		_, err := cert.ReadCN("/nonexistent/path/cert.pem")
		assert.Error(t, err)
	})

	t.Run("malformed PEM", func(t *testing.T) {
		t.Parallel()

		f := filepath.Join(t.TempDir(), "bad.pem")
		require.NoError(t, os.WriteFile(f, []byte("not a pem block"), 0o600))

		_, err := cert.ReadCN(f)
		assert.Error(t, err)
	})

	t.Run("valid PEM but not a certificate", func(t *testing.T) {
		t.Parallel()

		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		keyBytes, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)

		f := filepath.Join(t.TempDir(), "key.pem")
		require.NoError(t, os.WriteFile(f,
			pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}),
			0o600,
		))

		_, err = cert.ReadCN(f)
		assert.Error(t, err)
	})
}

func TestSplitCN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cn             string
		wantCluster    string
		wantCustomer   string
		wantErrContain string
	}{
		{
			name:         "normal certname",
			cn:           "staging.acme",
			wantCluster:  "staging",
			wantCustomer: "acme",
		},
		{
			name:         "cluster with multiple dots in customer is fine — only first dot splits",
			cn:           "prod.cust01.extra",
			wantCluster:  "prod",
			wantCustomer: "cust01.extra",
		},
		{
			name:           "no dot",
			cn:             "nodothere",
			wantErrContain: "no '.'",
		},
		{
			name:           "empty CN",
			cn:             "",
			wantErrContain: "no '.'",
		},
		{
			name:           "leading dot — empty clustername",
			cn:             ".customerid",
			wantErrContain: "clustername",
		},
		{
			name:           "trailing dot — empty customerid",
			cn:             "clustername.",
			wantErrContain: "customerid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotCluster, gotCustomer, err := cert.SplitCN(tc.cn)
			if tc.wantErrContain != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContain)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantCluster, gotCluster)
			assert.Equal(t, tc.wantCustomer, gotCustomer)
		})
	}
}
