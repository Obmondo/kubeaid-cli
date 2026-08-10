// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

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

// TestHydrateSSHKeyPairFromFileAsksWhenNoPathIsSet covers the config that
// names no privateKeyFilePath. The operator has the key — they just never said
// where — so the run asks instead of ending.
func TestHydrateSSHKeyPairFromFileAsksWhenNoPathIsSet(t *testing.T) {
	privatePEM, authorizedKey, fingerprint := sshTestKeyPair(t)
	keyPath := sshTestTempFile(t, "id_ed25519", privatePEM)

	restoreTerminal := stdinIsTerminal
	restorePrompt := promptSSHPrivateKeyPath
	t.Cleanup(func() {
		stdinIsTerminal = restoreTerminal
		promptSSHPrivateKeyPath = restorePrompt
	})

	asked := 0
	stdinIsTerminal = func() bool { return true }
	promptSSHPrivateKeyPath = func(path *string) error {
		asked++
		// The prompt opens on the usual location rather than an empty field.
		assert.Equal(t, defaultSSHPrivateKeyPath, *path)
		*path = keyPath
		return nil
	}

	sshKeyPairConfig := &config.SSHKeyPairConfig{}
	hydrateSSHKeyPairFromFile(sshKeyPairConfig)

	assert.Equal(t, 1, asked)
	assert.Equal(t, keyPath, sshKeyPairConfig.PrivateKeyFilePath, "the answer is kept for the rest of the run")
	assert.Equal(t, string(authorizedKey), sshKeyPairConfig.PublicKey)
	assert.Equal(t, fingerprint, sshKeyPairConfig.Fingerprint)
}

// TestHydrateSSHKeyPairFromFileDoesNotAskWhenAPathIsSet is the other half:
// a config that already answers the question is never interrupted.
func TestHydrateSSHKeyPairFromFileDoesNotAskWhenAPathIsSet(t *testing.T) {
	privatePEM, _, _ := sshTestKeyPair(t)

	restoreTerminal := stdinIsTerminal
	restorePrompt := promptSSHPrivateKeyPath
	t.Cleanup(func() {
		stdinIsTerminal = restoreTerminal
		promptSSHPrivateKeyPath = restorePrompt
	})

	stdinIsTerminal = func() bool { return true }
	promptSSHPrivateKeyPath = func(_ *string) error {
		t.Fatal("must not ask when general.yaml already names a path")
		return nil
	}

	hydrateSSHKeyPairFromFile(&config.SSHKeyPairConfig{
		PrivateKeyFilePath: sshTestTempFile(t, "id_ed25519", privatePEM),
	})
}

// TestValidateSSHPrivateKeyAtPath keeps a wrong answer inside the prompt,
// where it can be corrected, rather than accepting it and failing on the next
// line. Tilde expansion is covered here too: it is the form most keys are
// named by, and os.ReadFile does not do it.
func TestValidateSSHPrivateKeyAtPath(t *testing.T) {
	privatePEM, _, _ := sshTestKeyPair(t)
	keyPath := sshTestTempFile(t, "id_ed25519", privatePEM)

	assert.NoError(t, validateSSHPrivateKeyAtPath(keyPath))
	assert.Error(t, validateSSHPrivateKeyAtPath(""), "an empty answer is not a path")
	assert.Error(t, validateSSHPrivateKeyAtPath(filepath.Join(t.TempDir(), "absent")))

	notAKey := sshTestTempFile(t, "notes.txt", []byte("this is not a key\n"))
	assert.Error(t, validateSSHPrivateKeyAtPath(notAKey))

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Error(t, validateSSHPrivateKeyAtPath("~/"+filepath.Base(t.TempDir())+"/definitely-absent"),
		"a ~ path must be resolved against %s, not read literally", home)
}
