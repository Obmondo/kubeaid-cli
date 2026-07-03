// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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

func TestAttachServersToVSwitch(t *testing.T) {
	t.Parallel()

	const vswitchID = 50

	inProcessBody := fmt.Sprintf(
		`{"error":{"status":409,"code":%q,"message":"in process"}}`,
		constants.HRobotVSwitchInProcessErrorCode,
	)
	emptyVSwitchBody := fmt.Sprintf(`{"id":%d,"server":[]}`, vswitchID)

	// serverJSON renders a GET /vswitch/{id} body with the given
	// servers all at the given status.
	serverJSON := func(status string, ids ...int) string {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf(`{"server_number":%d,"status":%q}`, id, status)
		}
		return fmt.Sprintf(`{"id":%d,"server":[%s]}`, vswitchID, strings.Join(parts, ","))
	}

	tests := []struct {
		name       string
		serverIDs  []string
		handler    http.HandlerFunc
		wantErr    bool
		wantErrMsg string
	}{
		{
			// POST all servers in one call, then poll until both are
			// "ready" — "in process" is not "done".
			name:      "attaches all servers and waits until ready",
			serverIDs: []string{"100", "200"},
			handler: func() http.HandlerFunc {
				var gets atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodPost:
						assert.Equal(t, "/vswitch/50/server", r.URL.Path)
						require.NoError(t, r.ParseForm())
						assert.ElementsMatch(t, []string{"100", "200"}, r.PostForm["server[]"])
						w.WriteHeader(http.StatusCreated)
					case http.MethodGet:
						assert.Equal(t, "/vswitch/50", r.URL.Path)
						switch gets.Add(1) {
						case 1:
							_, _ = fmt.Fprint(w, emptyVSwitchBody)
						case 2:
							_, _ = fmt.Fprint(w, serverJSON("in process", 100, 200))
						default:
							_, _ = fmt.Fprint(w, serverJSON("ready", 100, 200))
						}
					}
				}
			}(),
		},
		{
			name:      "all servers already ready, no POST",
			serverIDs: []string{"100", "200"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					t.Errorf("POST must not be called when all servers are ready")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = fmt.Fprint(w, serverJSON("ready", 100, 200))
			},
		},
		{
			// An "in process" server must be waited on, never re-POSTed.
			name:      "waits for in-process server without re-posting",
			serverIDs: []string{"100"},
			handler: func() http.HandlerFunc {
				var gets atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						t.Errorf("in-process server must not be re-POSTed")
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					if gets.Add(1) == 1 {
						_, _ = fmt.Fprint(w, serverJSON("in process", 100))
						return
					}
					_, _ = fmt.Fprint(w, serverJSON("ready", 100))
				}
			}(),
		},
		{
			// A "failed" server must be re-POSTed.
			name:      "re-posts failed server then waits until ready",
			serverIDs: []string{"100"},
			handler: func() http.HandlerFunc {
				var gets atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodPost:
						require.NoError(t, r.ParseForm())
						assert.Equal(t, []string{"100"}, r.PostForm["server[]"])
						w.WriteHeader(http.StatusCreated)
					case http.MethodGet:
						if gets.Add(1) == 1 {
							_, _ = fmt.Fprint(w, serverJSON("failed", 100))
							return
						}
						_, _ = fmt.Fprint(w, serverJSON("ready", 100))
					}
				}
			}(),
		},
		{
			// Only the un-attached server gets POSTed.
			name:      "posts only the pending server",
			serverIDs: []string{"100", "200"},
			handler: func() http.HandlerFunc {
				var gets atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodPost:
						require.NoError(t, r.ParseForm())
						assert.Equal(t, []string{"200"}, r.PostForm["server[]"])
						w.WriteHeader(http.StatusCreated)
					case http.MethodGet:
						if gets.Add(1) == 1 {
							_, _ = fmt.Fprint(w, serverJSON("ready", 100))
							return
						}
						_, _ = fmt.Fprint(w, serverJSON("ready", 100, 200))
					}
				}
			}(),
		},
		{
			// POST rejected with VSWITCH_IN_PROCESS is not fatal — the
			// next poll tick re-POSTs once the vSwitch settles.
			name:      "retries POST on VSWITCH_IN_PROCESS then succeeds",
			serverIDs: []string{"100"},
			handler: func() http.HandlerFunc {
				var gets, posts atomic.Int32
				return func(w http.ResponseWriter, r *http.Request) {
					switch r.Method {
					case http.MethodPost:
						if posts.Add(1) == 1 {
							w.WriteHeader(http.StatusConflict)
							_, _ = w.Write([]byte(inProcessBody))
							return
						}
						w.WriteHeader(http.StatusCreated)
					case http.MethodGet:
						if gets.Add(1) >= 3 {
							_, _ = fmt.Fprint(w, serverJSON("ready", 100))
							return
						}
						_, _ = fmt.Fprint(w, emptyVSwitchBody)
					}
				}
			}(),
		},
		{
			name:      "other 409 returns error",
			serverIDs: []string{"100"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprint(w, emptyVSwitchBody)
					return
				}
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":{"status":409,"code":"VSWITCH_SERVER_LIMIT_REACHED"}}`))
			},
			wantErr:    true,
			wantErrMsg: "unexpected status code 409",
		},
		{
			name:      "unexpected POST status returns error",
			serverIDs: []string{"100"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprint(w, emptyVSwitchBody)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
			},
			wantErr:    true,
			wantErrMsg: "unexpected status code 400",
		},
		{
			name:      "GET failure returns error",
			serverIDs: []string{"100"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			wantErrMsg: "getting VSwitch 50",
		},
		{
			name:      "no servers is a no-op",
			serverIDs: nil,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				t.Errorf("no request must be made for an empty server list")
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()
			h.sleepFunc = noopSleep

			err := h.AttachServersToVSwitch(context.Background(), tc.serverIDs, vswitchID)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}
