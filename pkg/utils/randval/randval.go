// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package randval generates random secret values used across
// kubeaid-cli — alphanumeric passwords for OIDC client secrets and
// Keycloak admin credentials, plus base64-encoded byte keys for
// symmetric crypto (e.g. NetBird's datastoreEncryptionKey).
//
// Used by the secrets-fill path that auto-populates blank fields
// in the operator's secrets.yaml on first run, so SealedSecrets
// render with stable plaintext across re-runs.
package randval

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
)

// passwordAlphabet keeps generated passwords shell- / YAML- /
// kubectl-safe. 32 chars from this alphabet gives ~190 bits of
// entropy — well past the 128-bit threshold that justifies storing
// the value rather than rotating it on every use.
const (
	passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	PasswordLength   = 32
)

// Password returns a fresh PasswordLength-character alphanumeric
// password from passwordAlphabet, drawn from crypto/rand.
func Password() (string, error) {
	alphabetLen := big.NewInt(int64(len(passwordAlphabet)))

	out := make([]byte, PasswordLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("reading random byte for password: %w", err)
		}
		out[i] = passwordAlphabet[n.Int64()]
	}
	return string(out), nil
}

// Base64Key returns byteLen random bytes encoded as standard-
// padding base64. Used where the consumer expects a fixed-byte-
// length key rather than a printable-charset password — e.g.
// NetBird's datastoreEncryptionKey, which the Mgmt server base64-
// decodes into a 32-byte AES key.
func Base64Key(byteLen int) (string, error) {
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("reading random bytes for base64 key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
