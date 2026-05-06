// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
)

// Alphanumerics avoid escaping issues when the password flows
// through Helm values, kubectl, and shell invocations. 32 chars
// from this alphabet gives ~190 bits of entropy.
const (
	passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	passwordLength   = 32
)

func generatePassword() (string, error) {
	alphabetLen := big.NewInt(int64(len(passwordAlphabet)))

	out := make([]byte, passwordLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("reading random byte for admin password: %w", err)
		}
		out[i] = passwordAlphabet[n.Int64()]
	}

	return string(out), nil
}

// generateBase64Key returns byteLen random bytes encoded as
// standard-padding base64. Used where the consumer expects a
// fixed-byte-length key rather than printable-charset password —
// e.g. NetBird's datastoreEncryptionKey, which the Mgmt server
// base64-decodes into a 32-byte AES key.
func generateBase64Key(byteLen int) (string, error) {
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("reading random bytes for base64 key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
