// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestRSAPublicKeyFromPrivateKeyFile(t *testing.T) {
	dir := t.TempDir()

	// An RSA deploy key yields its public half as an authorized_keys line.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rsaPath := filepath.Join(dir, "rsa_key")
	require.NoError(t, os.WriteFile(rsaPath, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	}), 0o600))
	material := rsaPublicKeyFromPrivateKeyFile(rsaPath)
	assert.True(t, strings.HasPrefix(material, "ssh-rsa "), "expected an ssh-rsa line, got %q", material)

	// An ed25519 deploy key can't be provisioned onto Azure VMs — the caller
	// must fall back to prompting.
	_, ed25519Key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ed25519PEM, err := ssh.MarshalPrivateKey(ed25519Key, "")
	require.NoError(t, err)
	ed25519Path := filepath.Join(dir, "ed25519_key")
	require.NoError(t, os.WriteFile(ed25519Path, pem.EncodeToMemory(ed25519PEM), 0o600))
	assert.Empty(t, rsaPublicKeyFromPrivateKeyFile(ed25519Path))

	// Missing / empty paths degrade to the prompt, never an error.
	assert.Empty(t, rsaPublicKeyFromPrivateKeyFile(""))
	assert.Empty(t, rsaPublicKeyFromPrivateKeyFile(filepath.Join(dir, "nope")))
}

func TestAzurePrompter_SummaryLines(t *testing.T) {
	tests := []struct {
		name string
		cfg  *PromptedConfig
		want []string
	}{
		{
			name: "all fields populated",
			cfg: &PromptedConfig{
				AzureLocation:     "westeurope",
				AzureCPVMSize:     "Standard_B2s",
				AzureCPDiskSizeGB: "128",
				AzureCPReplicas:   "1",
			},
			want: []string{
				"  Location:      westeurope",
				"  VM size:       Standard_B2s",
				"  Disk size:     128 GB",
				"  CP replicas:   1",
			},
		},
		{
			name: "disk size always carries GB suffix",
			cfg: &PromptedConfig{
				AzureCPDiskSizeGB: "",
			},
			want: []string{
				"  Location:      ",
				"  VM size:       ",
				"  Disk size:      GB",
				"  CP replicas:   ",
			},
		},
	}

	p := newAzureProvider()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.SummaryLines(tc.cfg))
		})
	}
}
