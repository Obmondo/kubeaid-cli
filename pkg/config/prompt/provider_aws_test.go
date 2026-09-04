// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

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
		{
			name: "a chosen profile is reported first",
			cfg: &PromptedConfig{
				AWSProfile:        "work",
				AWSRegion:         "eu-west-1",
				AWSCPInstanceType: "t3.medium",
				AWSCPReplicas:     "3",
			},
			want: []string{
				"  Profile:       work",
				"  Region:        eu-west-1",
				"  Instance type: t3.medium",
				"  CP replicas:   3",
			},
		},
		{
			name: "EKS with a chosen profile",
			cfg: &PromptedConfig{
				AWSProfile: "work",
				AWSEKS:     true,
				AWSRegion:  "eu-west-1",
			},
			want: []string{
				"  Profile:       work",
				"  Region:        eu-west-1",
				"  Control plane: EKS (managed by AWS)",
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

func TestParseAWSProfileNames(t *testing.T) {
	tests := []struct {
		name         string
		contents     string
		isConfigFile bool
		want         []string
	}{
		{
			name: "config file: default plus prefixed profiles",
			contents: strings.Join([]string{
				"[default]",
				"region = eu-west-1",
				"[profile staging]",
				"region = eu-central-1",
				"  [profile prod]  ",
				"region = us-east-1",
			}, "\n"),
			isConfigFile: true,
			want:         []string{"default", "staging", "prod"},
		},
		{
			name: "config file: non-profile sections are skipped",
			contents: strings.Join([]string{
				"[sso-session corp]",
				"sso_region = eu-west-1",
				"[services localstack]",
				"[profile staging]",
			}, "\n"),
			isConfigFile: true,
			want:         []string{"staging"},
		},
		{
			// Without the `profile ` prefix, `[staging]` in config is not a
			// profile the SDK would resolve — only `[default]` is.
			name:         "config file: a bare section is not a profile",
			contents:     "[staging]\n[default]\n",
			isConfigFile: true,
			want:         []string{"default"},
		},
		{
			name:         "credentials file: every section is a profile",
			contents:     "[default]\naws_access_key_id = A\n\n[work]\naws_access_key_id = B\n",
			isConfigFile: false,
			want:         []string{"default", "work"},
		},
		{
			name:         "quoted profile names are unquoted",
			contents:     "[profile \"two words\"]\n",
			isConfigFile: true,
			want:         []string{"two words"},
		},
		{
			name:         "comments and blank lines are ignored",
			contents:     "# [profile commented]\n\n; another comment\n[profile real]\n",
			isConfigFile: true,
			want:         []string{"real"},
		},
		{
			name:         "empty file yields nothing",
			contents:     "",
			isConfigFile: true,
			want:         nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseAWSProfileNames(tc.contents, tc.isConfigFile))
		})
	}
}

// writeAWSFiles points HOME at a temp dir and writes the given ~/.aws files
// into it. An empty string means "don't write this file".
func writeAWSFiles(t *testing.T, configContents, credentialsContents string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	// Nothing from the developer's shell may leak into the decision.
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws"), 0o700))

	for name, contents := range map[string]string{
		"config":      configContents,
		"credentials": credentialsContents,
	} {
		if contents == "" {
			continue
		}
		require.NoError(t, os.WriteFile(
			filepath.Join(home, ".aws", name), []byte(contents), 0o600,
		))
	}
}

func TestDetectAWSProfiles(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		credentials string
		want        []string
	}{
		{
			name: "no AWS files means no profiles",
			want: []string{},
		},
		{
			name:   "single default profile",
			config: "[default]\nregion = eu-west-1\n",
			want:   []string{"default"},
		},
		{
			name:   "several profiles are sorted",
			config: "[default]\n[profile staging]\n[profile prod]\n",
			want:   []string{"default", "prod", "staging"},
		},
		{
			name:        "profiles from both files are merged and de-duplicated",
			config:      "[default]\n[profile staging]\n",
			credentials: "[default]\n[work]\n",
			want:        []string{"default", "staging", "work"},
		},
		{
			name:        "credentials-only setup is picked up",
			credentials: "[personal]\n[work]\n",
			want:        []string{"personal", "work"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeAWSFiles(t, tc.config, tc.credentials)

			assert.Equal(t, tc.want, detectAWSProfiles())
		})
	}
}

func TestPreferredAWSProfile(t *testing.T) {
	tests := []struct {
		name     string
		profiles []string
		envVar   string
		want     string
	}{
		{
			name:     "AWS_PROFILE wins when it is one of the profiles",
			profiles: []string{"default", "prod", "staging"},
			envVar:   "prod",
			want:     "prod",
		},
		{
			name:     "AWS_PROFILE naming an unknown profile is ignored",
			profiles: []string{"default", "prod"},
			envVar:   "gone",
			want:     "default",
		},
		{
			name:     "default is preferred when present",
			profiles: []string{"acme", "default"},
			want:     "default",
		},
		{
			name:     "first profile otherwise",
			profiles: []string{"acme", "prod"},
			want:     "acme",
		},
		{
			name:     "no profiles yields nothing",
			profiles: nil,
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AWS_PROFILE", tc.envVar)

			assert.Equal(t, tc.want, preferredAWSProfile(tc.profiles))
		})
	}
}

// chooseAWSProfile only opens a form when there is more than one profile — the
// no-profile and single-profile paths must settle without any input.
func TestChooseAWSProfileNonInteractivePaths(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		credentials    string
		cfg            *PromptedConfig
		wantProfile    string
		wantPromptKeys bool
	}{
		{
			name:           "nothing in ~/.aws falls back to typing keys in",
			wantPromptKeys: true,
		},
		{
			name:        "a lone default profile is left to the SDK",
			config:      "[default]\nregion = eu-west-1\n",
			wantProfile: "",
		},
		{
			name:        "a lone named profile is pinned, since there is no [default] to resolve",
			credentials: "[work]\naws_access_key_id = A\n",
			wantProfile: "work",
		},
		{
			// Keys already in the config win at bootstrap, so naming a profile
			// beside them would be a lie.
			name:        "a lone named profile is not pinned over existing keys",
			credentials: "[work]\naws_access_key_id = A\n",
			cfg:         &PromptedConfig{AWSAccessKeyID: "AKIAEXISTING"},
			wantProfile: "",
		},
		{
			// The profile can no longer resolve — it must not survive.
			name:           "a stale profile is dropped when ~/.aws is gone",
			cfg:            &PromptedConfig{AWSProfile: "gone"},
			wantProfile:    "",
			wantPromptKeys: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeAWSFiles(t, tc.config, tc.credentials)

			cfg := tc.cfg
			if cfg == nil {
				cfg = &PromptedConfig{}
			}

			promptForKeys, err := chooseAWSProfile(cfg)

			require.NoError(t, err)
			assert.Equal(t, tc.wantPromptKeys, promptForKeys)
			assert.Equal(t, tc.wantProfile, cfg.AWSProfile)
		})
	}
}

// Credentials exported in the shell are worth confirming rather than retyping,
// but they must reach secrets.yaml : env vars last one shell session, and
// `cluster bootstrap` needs them on every later run.
func TestChooseAWSProfilePrefillsFromEnv(t *testing.T) {
	tests := []struct {
		name        string
		credentials string
		// resumed stands in for a secrets.yaml recovered from a previous run.
		resumed          *PromptedConfig
		wantPromptKeys   bool
		wantAccessKeyID  string
		wantSecretKey    string
		wantSessionToken string
	}{
		{
			name:             "no ~/.aws seeds the inputs from the environment",
			wantPromptKeys:   true,
			wantAccessKeyID:  "AKIAFROMENV",
			wantSecretKey:    "env-secret",
			wantSessionToken: "env-token",
		},
		{
			name: "credentials already in the config win over the environment",
			resumed: &PromptedConfig{
				AWSAccessKeyID:     "AKIAFROMCONFIG",
				AWSSecretAccessKey: "config-secret",
			},
			wantPromptKeys:  true,
			wantAccessKeyID: "AKIAFROMCONFIG",
			wantSecretKey:   "config-secret",
			// Blank in the config, so the environment still fills this one in.
			wantSessionToken: "env-token",
		},
		{
			// A profile is chosen, so no keys are collected — seeding them
			// would put keys in secrets.yaml that silently beat the profile.
			name:           "a resolvable profile leaves the environment alone",
			credentials:    "[work]\naws_access_key_id = A\n",
			wantPromptKeys: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeAWSFiles(t, "", tc.credentials)
			t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFROMENV")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
			t.Setenv("AWS_SESSION_TOKEN", "env-token")

			cfg := tc.resumed
			if cfg == nil {
				cfg = &PromptedConfig{}
			}

			promptForKeys, err := chooseAWSProfile(cfg)

			require.NoError(t, err)
			assert.Equal(t, tc.wantPromptKeys, promptForKeys)
			assert.Equal(t, tc.wantAccessKeyID, cfg.AWSAccessKeyID)
			assert.Equal(t, tc.wantSecretKey, cfg.AWSSecretAccessKey)
			assert.Equal(t, tc.wantSessionToken, cfg.AWSSessionToken)
		})
	}
}

func TestAWSInstanceTypeIsARM(t *testing.T) {
	for _, instanceType := range []string{
		"c7g.xlarge", "t4g.medium", "m7g.large", "c7gn.xlarge", "x2gd.large",
		"im4gn.xlarge", "g5g.xlarge", "hpc7g.4xlarge", "a1.large", "r8g.medium",
	} {
		assert.True(t, awsInstanceTypeIsARM(instanceType), instanceType)
	}
	for _, instanceType := range []string{
		"t3.medium", "m5.large", "c5.xlarge", "g4dn.xlarge", "g5.xlarge",
		"m6i.large", "r5ad.large",
	} {
		assert.False(t, awsInstanceTypeIsARM(instanceType), instanceType)
	}
}

func TestUbuntuProductForInstanceType(t *testing.T) {
	assert.Equal(t, ubuntuProductARM64, ubuntuProductForInstanceType("t4g.medium"))
	assert.Equal(t, ubuntuProductAMD64, ubuntuProductForInstanceType("c6i.xlarge"))
}

func TestArchForProduct(t *testing.T) {
	assert.Equal(t, "arm64", archForProduct(ubuntuProductARM64))
	assert.Equal(t, "amd64", archForProduct(ubuntuProductAMD64))
}

func TestResolveUbuntuAMI(t *testing.T) {
	index := &awsSimplestreamsIndex{
		Products: map[string]awsSimplestreamsProduct{
			ubuntuProductARM64: {
				Versions: map[string]awsSimplestreamsVersion{
					"20240201": {
						Items: map[string]awsSimplestreamsItem{
							"eu-west-1": {ID: "ami-arm-eu", CRSN: "eu-west-1", RootStore: "ssd", Virt: "hvm"},
						},
					},
				},
			},
		},
	}

	assert.Equal(t, "ami-arm-eu", resolveUbuntuAMI(index, ubuntuProductARM64, "eu-west-1", "control-plane"))
	// Region not covered by the product.
	assert.Empty(t, resolveUbuntuAMI(index, ubuntuProductARM64, "ap-south-1", "control-plane"))
	// Product missing from the index.
	assert.Empty(t, resolveUbuntuAMI(index, ubuntuProductAMD64, "eu-west-1", "worker node-group"))
	// Index fetch failed → nil index.
	assert.Empty(t, resolveUbuntuAMI(nil, ubuntuProductARM64, "eu-west-1", "control-plane"))
}

func TestFetchLatestUbuntuAMIsReturnsLatestHVMSSDImagesByRegion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"products": {
				"com.ubuntu.cloud:server:26.04:arm64": {
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

	index, err := fetchUbuntuSimplestreamsIndex(context.Background(), clientForTestServer(server))
	require.NoError(t, err)
	amis, err := latestUbuntuAMIs(index, ubuntuProductARM64)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"eu-west-1": "ami-latest-eu",
		"us-east-1": "ami-latest-us",
	}, amis)
}

func TestFetchLatestUbuntuAMIsReturnsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := fetchUbuntuSimplestreamsIndex(context.Background(), clientForTestServer(server))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestFetchLatestUbuntuAMIsReturnsErrorForMissingProduct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"products": {}}`))
	}))
	defer server.Close()

	index, err := fetchUbuntuSimplestreamsIndex(context.Background(), clientForTestServer(server))
	require.NoError(t, err)

	_, err = latestUbuntuAMIs(index, ubuntuProductARM64)

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
