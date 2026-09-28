// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package iris

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pingPath = "/api/ping"

func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pingPath || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","message":"pong","data":[]}`))
	}))
	defer srv.Close()
	require.NoError(t, (&Client{BaseURL: srv.URL, APIKey: "good", HTTP: srv.Client()}).Ping(context.Background()))
	assert.Error(t, (&Client{BaseURL: srv.URL, APIKey: "bad", HTTP: srv.Client()}).Ping(context.Background()))
}
