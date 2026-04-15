// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
)

func newTestHetznerWithRobotServer(handler http.Handler) (*Hetzner, *httptest.Server) {
	server := httptest.NewServer(handler)
	robotClient := resty.New().
		SetBaseURL(server.URL).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json")
	return &Hetzner{robotClient: robotClient}, server
}

func TestActivateLinuxInstallation(t *testing.T) {
	t.Run("sends correct form params and succeeds on HTTP 200", func(t *testing.T) {
		var capturedPath string
		var capturedFormValues map[string][]string

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			err := r.ParseForm()
			assert.NoError(t, err)
			capturedFormValues = r.PostForm

			w.WriteHeader(http.StatusOK)
			_, err = fmt.Fprint(w,
				`{"linux":{"dist":"Ubuntu 24.04 LTS minimal","arch":64,"lang":"en",`+
					`"active":true,"password":"testpw","authorized_key":["ab:cd:ef"],"host_key":[]}}`,
			)
			assert.NoError(t, err)
		})

		h, server := newTestHetznerWithRobotServer(handler)
		defer server.Close()

		ctx := context.Background()
		h.activateLinuxInstallation(ctx, "12345", "Ubuntu 24.04 LTS minimal", "ab:cd:ef")

		assert.Equal(t, "/boot/12345/linux", capturedPath)
		assert.Equal(t, []string{"Ubuntu 24.04 LTS minimal"}, capturedFormValues["dist"])
		assert.Equal(t, []string{"64"}, capturedFormValues["arch"])
		assert.Equal(t, []string{"en"}, capturedFormValues["lang"])
		assert.Equal(t, []string{"ab:cd:ef"}, capturedFormValues["authorized_key[]"])
	})
}

func TestResetServer(t *testing.T) {
	t.Run("sends hw reset type and succeeds on HTTP 200", func(t *testing.T) {
		var capturedPath string
		var capturedFormValues map[string][]string

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			err := r.ParseForm()
			assert.NoError(t, err)
			capturedFormValues = r.PostForm

			w.WriteHeader(http.StatusOK)
			_, err = fmt.Fprint(w, `{"reset":{"server_ip":"1.2.3.4","type":"hw"}}`)
			assert.NoError(t, err)
		})

		h, server := newTestHetznerWithRobotServer(handler)
		defer server.Close()

		ctx := context.Background()
		h.resetServer(ctx, "12345")

		assert.Equal(t, "/reset/12345", capturedPath)
		assert.Equal(t, []string{"hw"}, capturedFormValues["type"])
	})
}
