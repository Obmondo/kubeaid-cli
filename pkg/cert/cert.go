// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

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

	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in cert file %q", path)
	}

	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parsing cert file %q: %w", path, err)
	}

	return c.Subject.CommonName, nil
}

// SplitCN bisects a puppet certname on the first '.' and returns
// (clustername, customerid). Returns an error when no '.' is present or either
// component is empty.
func SplitCN(cn string) (clusterName, customerID string, err error) {
	idx := -1
	for i, r := range cn {
		if r == '.' {
			idx = i
			break
		}
	}

	if idx < 0 {
		return "", "", fmt.Errorf("cert CN %q has no '.': expected <clustername>.<customerid>", cn)
	}

	clusterName = cn[:idx]
	customerID = cn[idx+1:]

	if clusterName == "" {
		return "", "", fmt.Errorf("cert CN %q: clustername (left of '.') is empty", cn)
	}

	if customerID == "" {
		return "", "", fmt.Errorf("cert CN %q: customerid (right of '.') is empty", cn)
	}

	return clusterName, customerID, nil
}
