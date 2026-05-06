// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

const robotVSwitchPath = "/vswitch"

// Mutates vSwitchID and config.ParsedGeneralConfig — sequential only.
func TestCreateVSwitch(t *testing.T) {
	setupVSwitchConfig := func(t *testing.T) {
		t.Helper()
		savedConfig := config.ParsedGeneralConfig
		savedVSwitchID := vSwitchID
		t.Cleanup(func() {
			config.ParsedGeneralConfig = savedConfig
			vSwitchID = savedVSwitchID
		})

		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cloud: config.CloudConfig{
				Hetzner: &config.HetznerConfig{
					BareMetal: &config.HetznerBareMetalConfig{
						VSwitch: &config.VSwitchConfig{
							Name:   "test-vswitch",
							VLANID: 4000,
						},
					},
				},
			},
		}
		vSwitchID = 0
	}

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantID     int
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "VSwitch already exists with matching name and VLANID",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(ListVSwitchResponseBody{
						{ID: 42, Name: "test-vswitch", VLANID: 4000, Cancelled: false},
					})
					_, _ = w.Write(body)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantID: 42,
		},
		{
			name: "VSwitch does not exist and is created",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `[]`)

				case r.URL.Path == robotVSwitchPath && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(CreateVSwitchResponseBody{
						ID: 99, Name: "test-vswitch", VLANID: 4000,
					})
					_, _ = w.Write(body)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantID: 99,
		},
		{
			name: "HTTP error on list VSwitches",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			wantErrMsg: "listing VSwitches: unexpected status code 500",
		},
		{
			name: "conflicting VSwitch name with same VLANID",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(ListVSwitchResponseBody{
						{ID: 10, Name: "other-vswitch", VLANID: 4000, Cancelled: false},
					})
					_, _ = w.Write(body)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: "a different VSwitch",
		},
		{
			name: "cancelled VSwitch returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(ListVSwitchResponseBody{
						{ID: 42, Name: "test-vswitch", VLANID: 4000, Cancelled: true},
					})
					_, _ = w.Write(body)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: "cancelled",
		},
		{
			name: "HTTP error on create VSwitch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `[]`)

				case r.URL.Path == robotVSwitchPath && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusBadRequest)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr:    true,
			wantErrMsg: "creating VSwitch: unexpected status code 400",
		},
		{
			name: "invalid JSON in list response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `not-json`)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: "unmarshalling list VSwitch response body",
		},
		{
			name: "invalid JSON in create response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == robotVSwitchPath && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `[]`)

				case r.URL.Path == robotVSwitchPath && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `bad-json`)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr:    true,
			wantErrMsg: "unmarshalling create VSwitch response body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupVSwitchConfig(t)

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			got, err := h.CreateVSwitch(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got)
		})
	}
}

func TestAttachServerToVSwitch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverID   string
		vswitchID  int
		handler    http.HandlerFunc
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:      "success on HTTP 201",
			serverID:  "100",
			vswitchID: 50,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/vswitch/50/server", r.URL.Path)
				w.WriteHeader(http.StatusCreated)
			},
		},
		{
			name:      "conflict returns success (HTTP 409)",
			serverID:  "100",
			vswitchID: 50,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusConflict)
			},
		},
		{
			name:      "unexpected status returns error",
			serverID:  "100",
			vswitchID: 50,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			},
			wantErr:    true,
			wantErrMsg: "unexpected status code 400",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			err := h.AttachServerToVSwitch(context.Background(), tc.serverID, tc.vswitchID)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
