// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"os"
	"path/filepath"
	"testing"

	validatorV10 "github.com/go-playground/validator/v10"
	nonStandardValidators "github.com/go-playground/validator/v10/non-standard/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

// TestRenderSecretsQuotesYAMLMetacharacters locks in the fix for a
// regression where Hetzner Robot usernames starting with '#' rendered
// as YAML comments — `user: #foo` parsed as `user:` (blank) +
// trailing comment, failing the Robot.User notblank validator at
// bootstrap. Every secret value is now %q-quoted so YAML
// metacharacters (#, :, -, &, *, !, leading whitespace, etc.) flow
// through verbatim.
func TestRenderSecretsQuotesYAMLMetacharacters(t *testing.T) {
	cases := []struct {
		name         string
		user         string
		password     string
		wantUser     string
		wantPassword string
	}{
		{
			name:         "username starts with # (real Hetzner Robot format)",
			user:         "#ws+QJdcac8L",
			password:     "XHUhViIJ7b86VE7EIpT9",
			wantUser:     "#ws+QJdcac8L",
			wantPassword: "XHUhViIJ7b86VE7EIpT9",
		},
		{
			name:         "password contains a colon",
			user:         "user1",
			password:     "p:ass:word",
			wantUser:     "user1",
			wantPassword: "p:ass:word",
		},
		{
			name:         "username starts with -",
			user:         "-dash-leader",
			password:     "secret",
			wantUser:     "-dash-leader",
			wantPassword: "secret",
		},
		{
			name:         "password contains a double quote",
			user:         "user2",
			password:     `pass"word`,
			wantUser:     "user2",
			wantPassword: `pass"word`,
		},
		{
			name:         "password contains a backslash",
			user:         "user3",
			password:     `pass\word`,
			wantUser:     "user3",
			wantPassword: `pass\word`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &PromptedConfig{
				CloudProvider:        "hetzner",
				HetznerAPIToken:      "fake-api-token",
				HetznerRobotUser:     tc.user,
				HetznerRobotPassword: tc.password,
			}
			dir := t.TempDir()
			require.NoError(t, writeConfigFiles(dir, cfg))

			body, err := os.ReadFile(filepath.Join(dir, "secrets.yaml"))
			require.NoError(t, err)

			parsed := &config.SecretsConfig{}
			require.NoError(t, yaml.Unmarshal(body, parsed),
				"rendered secrets.yaml must parse cleanly — raw bytes:\n%s", string(body))

			require.NotNil(t, parsed.Hetzner)
			require.NotNil(t, parsed.Hetzner.Robot,
				"robot block must be present and parseable — raw bytes:\n%s", string(body))
			assert.Equal(t, tc.wantUser, parsed.Hetzner.Robot.User)
			assert.Equal(t, tc.wantPassword, parsed.Hetzner.Robot.Password)

			// Same struct validator the bootstrap runs — would have
			// caught the # bug as Robot.User notblank failure.
			v := validatorV10.New(validatorV10.WithRequiredStructEnabled())
			require.NoError(t, v.RegisterValidation("notblank", nonStandardValidators.NotBlank))
			assert.NoError(t, v.Struct(parsed),
				"validator.Struct must pass — raw bytes:\n%s", string(body))
		})
	}
}

// TestRenderSecretsPureBareMetalSkipsAPIToken proves that the
// prompt-time form's "hide API-token input for pure-BM" pairs
// cleanly with the secrets template — no apiToken line is rendered
// for pure bare-metal clusters, since that mode doesn't touch the
// HCloud API. Robot creds still render. The resulting YAML must
// parse and satisfy struct validation (notblank is gone from
// APIToken; only cross-mode validation in pkg/config/parser/
// validate.go enforces it for hcloud / hybrid).
func TestRenderSecretsPureBareMetalSkipsAPIToken(t *testing.T) {
	cfg := &PromptedConfig{
		CloudProvider:        "hetzner",
		HetznerMode:          "bare-metal",
		HetznerAPIToken:      "", // pure-BM: input hidden, value empty
		HetznerRobotUser:     "u",
		HetznerRobotPassword: "p",
	}
	dir := t.TempDir()
	require.NoError(t, writeConfigFiles(dir, cfg))

	body, err := os.ReadFile(filepath.Join(dir, "secrets.yaml"))
	require.NoError(t, err)

	assert.NotContains(t, string(body), "apiToken",
		"pure-BM secrets.yaml must not carry an apiToken line — raw bytes:\n%s", string(body))
	assert.Contains(t, string(body), `user: "u"`)
	assert.Contains(t, string(body), `password: "p"`)

	parsed := &config.SecretsConfig{}
	require.NoError(t, yaml.Unmarshal(body, parsed))
	require.NotNil(t, parsed.Hetzner, "hetzner: block must still render")
	assert.Empty(t, parsed.Hetzner.APIToken, "APIToken must parse as empty")
	require.NotNil(t, parsed.Hetzner.Robot)
	assert.Equal(t, "u", parsed.Hetzner.Robot.User)

	// Struct validation must pass — apiToken's notblank tag is gone,
	// and the cross-mode check in pkg/config/parser/validate.go only
	// fires when UsingHCloud() (irrelevant for this pure-BM case).
	v := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	require.NoError(t, v.RegisterValidation("notblank", nonStandardValidators.NotBlank))
	assert.NoError(t, v.Struct(parsed))
}

// TestRenderSecretsOmitsRobotWhenIncomplete proves the defensive
// guard: if user or password is missing (a half-state we don't
// expect to hit but shouldn't render either), the whole robot block
// is omitted. Hetzner.Robot stays nil so the struct validator skips
// it cleanly instead of erroring on a half-filled block.
func TestRenderSecretsOmitsRobotWhenIncomplete(t *testing.T) {
	cases := []struct {
		name     string
		user     string
		password string
	}{
		{name: "both empty", user: "", password: ""},
		{name: "user empty, password set", user: "", password: "p"},
		{name: "user set, password empty", user: "u", password: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &PromptedConfig{
				CloudProvider:        "hetzner",
				HetznerAPIToken:      "fake-api-token",
				HetznerRobotUser:     tc.user,
				HetznerRobotPassword: tc.password,
			}
			dir := t.TempDir()
			require.NoError(t, writeConfigFiles(dir, cfg))

			body, err := os.ReadFile(filepath.Join(dir, "secrets.yaml"))
			require.NoError(t, err)
			assert.NotContains(t, string(body), "robot:",
				"robot block must be omitted when either credential is empty")

			parsed := &config.SecretsConfig{}
			require.NoError(t, yaml.Unmarshal(body, parsed))
			require.NotNil(t, parsed.Hetzner)
			assert.Nil(t, parsed.Hetzner.Robot,
				"parsed Hetzner.Robot must be nil so the validator skips it")
		})
	}
}
