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

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

const robotKeyPath = "/key"

//nolint:gocognit
func TestCreateHetznerBareMetalSSHKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		keyName    string
		sshKeyPair config.SSHKeyPairConfig
		handler    http.HandlerFunc
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "key already exists with matching name and fingerprint",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
				PublicKey:   "ssh-rsa AAAA...",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotKeyPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(GetKeysResponse{
						{Key: Key{Name: "my-key", Fingerprint: "aa:bb:cc:dd"}},
					})
					_, _ = w.Write(body)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
		},
		{
			name:    "no keys exist (404) then creates new key",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
				PublicKey:   "ssh-rsa AAAA...",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == robotKeyPath && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusNotFound)

				case r.URL.Path == robotKeyPath && r.Method == http.MethodPost:
					err := r.ParseForm() //nolint:gosec
					if err == nil {
						assert.Equal(t, "my-key", r.PostFormValue("name"))          //nolint:gosec
						assert.Equal(t, "ssh-rsa AAAA...", r.PostFormValue("data")) //nolint:gosec
					}
					w.WriteHeader(http.StatusCreated)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name:    "no matching key in list then creates new key",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
				PublicKey:   "ssh-rsa AAAA...",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == robotKeyPath && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(GetKeysResponse{
						{Key: Key{Name: "other-key", Fingerprint: "xx:yy:zz"}},
					})
					_, _ = w.Write(body)

				case r.URL.Path == robotKeyPath && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusCreated)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name:    "mismatched fingerprint with same name returns error",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotKeyPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					body, _ := json.Marshal(GetKeysResponse{
						{Key: Key{Name: "my-key", Fingerprint: "xx:yy:zz"}},
					})
					_, _ = w.Write(body)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: "same name but different fingerprint",
		},
		{
			name:    "unexpected status on list returns error",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			wantErrMsg: "unexpected response status code 500",
		},
		{
			name:    "invalid JSON on list returns error",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == robotKeyPath && r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `not-json`)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			wantErrMsg: "unmarshalling keys response",
		},
		{
			name:    "create key fails with unexpected status",
			keyName: "my-key",
			sshKeyPair: config.SSHKeyPairConfig{
				Fingerprint: "aa:bb:cc:dd",
				PublicKey:   "ssh-rsa AAAA...",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == robotKeyPath && r.Method == http.MethodGet:
					w.WriteHeader(http.StatusNotFound)

				case r.URL.Path == robotKeyPath && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusBadRequest)

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantErr:    true,
			wantErrMsg: "creating Hetzner Bare Metal SSH key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, server := newTestHetznerWithRobotServer(tc.handler)
			defer server.Close()

			err := h.CreateHetznerBareMetalSSHKey(context.Background(), tc.keyName, tc.sshKeyPair)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestCreateHetznerBareMetalSSHKey_ReusesExistingByFingerprint covers
// the same-fingerprint-different-name idempotency path separately
// because it has to mutate the package-level config.ParsedGeneralConfig
// — running in the same parallel group as the rest would race that
// global. The reuse path is the common case for operators sharing one
// SSH key (yubikey, or a hand-onboarded entry from a prior cluster)
// across multiple Robot accounts / clusters.
func TestCreateHetznerBareMetalSSHKey_ReusesExistingByFingerprint(t *testing.T) {
	orig := config.ParsedGeneralConfig
	t.Cleanup(func() { config.ParsedGeneralConfig = orig })

	const existingName = "operator-key"
	config.ParsedGeneralConfig = &config.GeneralConfig{
		Cloud: config.CloudConfig{
			Hetzner: &config.HetznerConfig{
				SSHKeyPair: config.HetznerSSHKeyPair{Name: "my-key"},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == robotKeyPath && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			body, _ := json.Marshal(GetKeysResponse{
				{Key: Key{Name: existingName, Fingerprint: "aa:bb:cc:dd"}},
			})
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	h, server := newTestHetznerWithRobotServer(handler)
	defer server.Close()

	err := h.CreateHetznerBareMetalSSHKey(context.Background(), "my-key", config.SSHKeyPairConfig{
		Fingerprint: "aa:bb:cc:dd",
	})
	require.NoError(t, err)
	assert.Equal(t, existingName,
		config.ParsedGeneralConfig.Cloud.Hetzner.SSHKeyPair.Name,
		"cfg.SSHKeyPair.Name must be rewritten to the Robot entry's name")
}
