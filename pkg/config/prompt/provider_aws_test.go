// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSPrompter_SummaryLines(t *testing.T) {
	tests := []struct {
		name string
		cfg  *PromptedConfig
		want []string
	}{
		{
			name: "all fields populated",
			cfg: &PromptedConfig{
				AWSRegion:         "eu-west-1",
				AWSCPInstanceType: "t3.medium",
				AWSCPReplicas:     "3",
			},
			want: []string{
				"  Region:        eu-west-1",
				"  Instance type: t3.medium",
				"  CP replicas:   3",
			},
		},
		{
			name: "empty values still render",
			cfg:  &PromptedConfig{},
			want: []string{
				"  Region:        ",
				"  Instance type: ",
				"  CP replicas:   ",
			},
		},
	}

	p := newAWSProvider()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.SummaryLines(tc.cfg))
		})
	}
}

func TestDetectAWSCredentials(t *testing.T) {
	tests := []struct {
		name      string
		setupHome func(t *testing.T) string
		wantOK    bool
		wantBase  string
	}{
		{
			name: "credentials file is detected",
			setupHome: func(t *testing.T) string {
				home := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o700))
				require.NoError(t, os.WriteFile(
					filepath.Join(home, ".aws", "credentials"),
					[]byte("[default]\n"),
					0o600,
				))
				return home
			},
			wantOK:   true,
			wantBase: "credentials",
		},
		{
			name: "config file is detected when credentials missing",
			setupHome: func(t *testing.T) string {
				home := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o700))
				require.NoError(t, os.WriteFile(
					filepath.Join(home, ".aws", "config"),
					[]byte("[default]\nregion=eu-west-1\n"),
					0o600,
				))
				return home
			},
			wantOK:   true,
			wantBase: "config",
		},
		{
			name: "no AWS files means not detected",
			setupHome: func(t *testing.T) string {
				return t.TempDir()
			},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := tc.setupHome(t)
			t.Setenv("HOME", home)

			source, ok := detectAWSCredentials()
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantBase, filepath.Base(source))
			}
		})
	}
}

func TestFetchLatestUbuntu2404AMIsReturnsLatestHVMSSDImagesByRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"products": {
				"com.ubuntu.cloud:server:24.04:amd64": {
					"versions": {
						"20240101": {
							"items": {
								"old": {"id": "ami-old", "crsn": "eu-west-1", "root_store": "ssd", "virt": "hvm"}
							}
						},
						"20240201": {
							"items": {
								"eu-west-1": {"id": "ami-latest-eu", "crsn": "eu-west-1", "root_store": "ssd-gp3", "virt": "hvm"},
								"us-east-1": {"id": "ami-latest-us", "region": "us-east-1", "root_store": "ssd", "virt": "hvm"},
								"paravirtual": {"id": "ami-paravirtual", "crsn": "eu-central-1", "root_store": "ssd", "virt": "pv"},
								"instance-store": {"id": "ami-instance-store", "crsn": "ap-south-1", "root_store": "instance", "virt": "hvm"},
								"missing-region": {"id": "ami-missing-region", "root_store": "ssd", "virt": "hvm"}
							}
						}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	amis, err := fetchLatestUbuntu2404AMIs(context.Background(), clientForTestServer(server))
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"eu-west-1": "ami-latest-eu",
		"us-east-1": "ami-latest-us",
	}, amis)
}

func TestFetchLatestUbuntu2404AMIsReturnsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := fetchLatestUbuntu2404AMIs(context.Background(), clientForTestServer(server))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestFetchLatestUbuntu2404AMIsReturnsErrorForMissingProduct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"products": {}}`))
	}))
	defer server.Close()

	_, err := fetchLatestUbuntu2404AMIs(context.Background(), clientForTestServer(server))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

type rewriteTransport struct {
	serverURL string
	base      http.RoundTripper
}

func clientForTestServer(server *httptest.Server) *http.Client {
	return &http.Client{
		Transport: rewriteTransport{
			serverURL: server.URL,
			base:      http.DefaultTransport,
		},
	}
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	testReq := req.Clone(req.Context())
	testReq.URL.Scheme = "http"
	testReq.URL.Host = strings.TrimPrefix(rt.serverURL, "http://")
	return rt.base.RoundTrip(testReq)
}
