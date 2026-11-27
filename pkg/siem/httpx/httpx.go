// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package httpx builds HTTP clients for the SIEM component APIs, most
// of which serve certificates from a private CA.
package httpx

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// DefaultTimeout bounds a single API request.
const DefaultTimeout = 30 * time.Second

// Client returns an HTTP client trusting caFile (PEM, optional) in
// addition to the system roots. insecure disables verification and is
// meant for self-signed test setups only.
func Client(caFile string, insecure bool) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // explicit opt-in per component
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file %s: %w", caFile, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificate found in CA file " + caFile)
		}
		tlsCfg.RootCAs = pool
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("unexpected default HTTP transport")
	}
	t := transport.Clone()
	t.TLSClientConfig = tlsCfg
	return &http.Client{Transport: t, Timeout: DefaultTimeout}, nil
}
