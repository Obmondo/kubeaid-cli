// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// sshTestKeyPair generates an ed25519 key and returns its OpenSSH private
// key (PEM), its authorized_keys public key, and its legacy-MD5 fingerprint.
func sshTestKeyPair(t *testing.T) (privatePEM, authorizedKey []byte, fingerprint string) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	require.NoError(t, err)

	privateKeyBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	require.NoError(t, err)

	return pem.EncodeToMemory(privateKeyBlock),
		ssh.MarshalAuthorizedKey(sshPublicKey),
		ssh.FingerprintLegacyMD5(sshPublicKey)
}

func sshTestTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// hydrateSSHKeyPairFromFile must derive the public key and a legacy-MD5
// fingerprint from the private key file. The MD5 format is load-bearing:
// Hetzner's HCloud and Robot APIs match SSH keys on it.
func TestHydrateSSHKeyPairFromFile(t *testing.T) {
	privatePEM, authorizedKey, fingerprint := sshTestKeyPair(t)

	sshKeyPairConfig := &config.SSHKeyPairConfig{
		PrivateKeyFilePath: sshTestTempFile(t, "id_ed25519", privatePEM),
	}
	hydrateSSHKeyPairFromFile(sshKeyPairConfig)

	assert.Equal(t, string(authorizedKey), sshKeyPairConfig.PublicKey)
	assert.Equal(t, fingerprint, sshKeyPairConfig.Fingerprint)
	assert.NotEmpty(t, sshKeyPairConfig.PrivateKey)
}

// publicKeyFileMatchesFingerprint underpins the Azure OpenID-provider public
// key validation (issue #14). It must read an authorized_keys .pub file and
// compare its legacy-MD5 fingerprint. The earlier code used ssh.ParsePublicKey
// (SSH wire format) and a SHA256 fingerprint, so the check never matched.
func TestPublicKeyFileMatchesFingerprint(t *testing.T) {
	_, authorizedKeyA, fingerprintA := sshTestKeyPair(t)
	_, authorizedKeyB, _ := sshTestKeyPair(t)

	t.Run("matching public key", func(t *testing.T) {
		path := sshTestTempFile(t, "match.pub", authorizedKeyA)

		matches, err := publicKeyFileMatchesFingerprint(path, fingerprintA)
		require.NoError(t, err)
		assert.True(t, matches)
	})

	t.Run("mismatched public key", func(t *testing.T) {
		path := sshTestTempFile(t, "mismatch.pub", authorizedKeyB)

		matches, err := publicKeyFileMatchesFingerprint(path, fingerprintA)
		require.NoError(t, err)
		assert.False(t, matches)
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := publicKeyFileMatchesFingerprint(
			filepath.Join(t.TempDir(), "does-not-exist.pub"), fingerprintA)
		assert.Error(t, err)
	})

	t.Run("malformed public key file", func(t *testing.T) {
		path := sshTestTempFile(t, "garbage.pub", []byte("not an ssh public key"))

		_, err := publicKeyFileMatchesFingerprint(path, fingerprintA)
		assert.Error(t, err)
	})
}
