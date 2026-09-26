// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientTrustsCAFile(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	// Without the CA the self-signed server is refused.
	plain, err := Client("", false)
	require.NoError(t, err)
	_, err = plain.Get(srv.URL)
	require.Error(t, err)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	block := &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(block), 0o600))
	withCA, err := Client(caFile, false)
	require.NoError(t, err)
	resp, err := withCA.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	insecure, err := Client("", true)
	require.NoError(t, err)
	resp, err = insecure.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
}

func TestClientBadCAFile(t *testing.T) {
	t.Parallel()
	_, err := Client(filepath.Join(t.TempDir(), "missing.pem"), false)
	require.Error(t, err)

	empty := filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(empty, []byte("not a cert"), 0o600))
	_, err = Client(empty, false)
	require.Error(t, err)
}
