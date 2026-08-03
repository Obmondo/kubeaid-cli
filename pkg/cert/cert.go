// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package cert provides helpers for reading PEM-encoded X.509 certificates.
package cert

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// ReadCN reads a PEM-encoded X.509 certificate at path and returns its Subject
// Common Name. Returns an error if the file cannot be read, contains no PEM
// block, or cannot be parsed as a certificate.
func ReadCN(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading cert file %q: %w", path, err)
	}

	cn, err := CN(data)
	if err != nil {
		return "", fmt.Errorf("cert file %q: %w", path, err)
	}
	return cn, nil
}

// CN returns the Subject Common Name of a PEM-encoded X.509 certificate held
// in memory. Same check as ReadCN, for callers holding bytes that have not
// been written to disk yet.
func CN(data []byte) (string, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block found")
	}

	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parsing certificate: %w", err)
	}

	return c.Subject.CommonName, nil
}
