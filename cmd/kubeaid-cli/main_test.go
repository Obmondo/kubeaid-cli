// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// runMainEnv makes this test binary behave as the CLI instead of running
	// tests, which is the only way to drive a command whose failure path is
	// os.Exit(1) — both PrepareClusterCommand and assert.AssertErrNil end
	// there, so an in-process call would take the test runner down with it.
	runMainEnv = "KUBEAID_CLI_TEST_RUN_MAIN"

	// The child goes through main() rather than a hand-assembled command
	// tree because main() is where cobra.EnableTraverseRunHooks is set, and
	// that flag is half the behaviour under test: with it, ClusterCmd's
	// PersistentPreRun runs before BootstrapCmd's own.
	//
	// certname and token are shaped like the real thing but name nothing:
	// no request that reaches a real API can be authorised by them.
	testCertname = "test-cluster.test-customer"
	testToken    = "not-a-real-token"

	// fetchFailure is what bootstrap reports once it has got as far as
	// asking Obmondo for the config; prepareFailure and configFilesNotFound
	// are the regression — the shared prepare step running first and exiting
	// on the empty configs directory a --token run always starts from.
	fetchFailure        = "Failed fetching cluster configuration from Obmondo"
	prepareFailure      = "Failed preparing config files"
	configFilesNotFound = "config files not found"
)

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()

		// main() returns only when Execute() succeeded, which none of these
		// runs do; exiting explicitly keeps a silent success from being read
		// as the test binary passing.
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// runCLI runs args in a child process holding nothing: an empty working
// directory, and an XDG config home with no saved cluster in it, so the
// configs directory really is the fresh machine the regression needs. The
// environment is built from scratch rather than inherited so a developer's
// own KUBEAID_CLI_TOKEN or saved configs cannot make a case pass.
func runCLI(t *testing.T, args ...string) (string, int) {
	_, output, exitCode := runCLIInHome(t, args...)

	return output, exitCode
}

// runCLIInHome is runCLI plus the home it ran against, for the cases that need
// to look at what the run wrote.
func runCLIInHome(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	home := t.TempDir()
	workingDirectory := t.TempDir()

	configsDirectory := filepath.Join(workingDirectory, "outputs", "configs")
	require.NoError(t, os.MkdirAll(configsDirectory, 0o700))
	entries, err := os.ReadDir(configsDirectory)
	require.NoError(t, err)
	require.Empty(t, entries, "the configs directory must be empty for this to be the regression")

	//nolint:gosec // os.Args[0] is this test binary.
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = workingDirectory
	cmd.Env = []string{
		runMainEnv + "=1",
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"PATH=" + os.Getenv("PATH"),
	}

	output, err := cmd.CombinedOutput()

	exitCode := 0
	var exitErr *exec.ExitError
	if err != nil {
		require.ErrorAs(t, err, &exitErr, "the CLI must exit, not fail to start: %s", output)
		exitCode = exitErr.ExitCode()
	}

	return home, string(output), exitCode
}

// servingAPI answers the install endpoint the way the portal does, so the fetch
// succeeds and the run goes on to write and parse what came back.
//
// The general.yaml it returns names a cluster and nothing else, deliberately:
// the point is to prove the config was fetched, written and parsed without a
// real token, so the run should fail in validation. Anything that bootstraps
// for real would need Docker and would not belong in a unit test.
func servingAPI(t *testing.T) string {
	t.Helper()

	const payload = `{"success":true,"data":{` +
		`"general_yaml":"cluster:\n  name: test-cluster\n",` +
		`"secrets_yaml":"{}\n"}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// unreachableAPI is a loopback address nothing can be listening on: port 1 is
// privileged and reserved, so this fails at dial without leaving the machine.
const unreachableAPI = "http://127.0.0.1:1"

// rejectingAPI stands in for the portal answering an expired or wrong token,
// so the fetch is reached and refused on the wire rather than only at dial —
// two independent ways of failing inside Fetch, one shared expectation.
func rejectingAPI(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// The regression: on a fresh machine the configs directory is empty, and
// EnableTraverseRunHooks means ClusterCmd's hook runs before BootstrapCmd's.
// Preparing there exited with "config files not found" before the config had
// been fetched, so every --token bootstrap died before it made a request.
//
// Asserted as the failure the run reaches: getting as far as the Obmondo
// fetch is the whole point, and a bootstrap that fails anywhere earlier has
// the bug back.
func TestBootstrapWithATokenReachesTheObmondoFetch(t *testing.T) {
	tests := []struct {
		name   string
		apiURL func(t *testing.T) string
	}{
		{
			name:   "API unreachable",
			apiURL: func(*testing.T) string { return unreachableAPI },
		},
		{
			name:   "API rejects the token",
			apiURL: rejectingAPI,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, exitCode := runCLI(t,
				"cluster", "bootstrap",
				"--token", testToken,
				"--certname", testCertname,
				"--obmondo-api-url", tc.apiURL(t),
			)

			assert.Equal(t, 1, exitCode, output)
			assert.Contains(t, output, fetchFailure, "bootstrap never got as far as asking Obmondo")

			assert.NotContains(t, output, configFilesNotFound,
				"the shared prepare step ran before the config was fetched — the empty configs directory killed the run")
			assert.NotContains(t, output, prepareFailure,
				"the shared prepare step ran before the config was fetched — the empty configs directory killed the run")
		})
	}
}

// The control the case above needs to mean anything: the same empty directory
// must still stop a subcommand that has no config of its own to fetch. Without
// this, "no config files not found" could just as well mean the harness never
// produces that message at all.
func TestOtherClusterSubcommandsStillFailOnAnEmptyConfigsDirectory(t *testing.T) {
	output, exitCode := runCLI(t, "cluster", "test")

	assert.Equal(t, 1, exitCode, output)
	assert.Contains(t, output, configFilesNotFound)
	assert.Contains(t, output, prepareFailure)
}

// A token names no cluster on its own, so this is refused before any request.
// It shares the guarded hook with the cases above, and would be reported as
// the missing config files if the parent prepared first.
func TestBootstrapWithATokenAndNoCertnameIsRefusedBeforeTheFetch(t *testing.T) {
	output, exitCode := runCLI(t,
		"cluster", "bootstrap",
		"--token", testToken,
		"--obmondo-api-url", unreachableAPI,
	)

	assert.Equal(t, 1, exitCode, output)
	assert.Contains(t, output, "--token does not say which cluster it is for")
	assert.NotContains(t, output, configFilesNotFound)
}

// The other half of the regression, and the half that needs no token at all:
// when the fetch succeeds the config it returns must reach disk and be parsed.
// A run that stops at the missing-config check never asked for it; a run that
// stops at the fetch never got it.
func TestBootstrapWithATokenWritesTheFetchedConfig(t *testing.T) {
	home, output, exitCode := runCLIInHome(t,
		"cluster", "bootstrap",
		"--token", testToken,
		"--certname", testCertname,
		"--obmondo-api-url", servingAPI(t),
	)

	assert.Equal(t, 1, exitCode, output)
	assert.NotContains(t, output, configFilesNotFound,
		"the shared prepare step ran before the config was fetched")
	assert.NotContains(t, output, fetchFailure,
		"the fetch itself failed, so this says nothing about what happens after it")

	written, err := filepath.Glob(filepath.Join(home, ".config", "kubeaid-cli", "*", "configs", "general.yaml"))
	require.NoError(t, err)
	assert.NotEmpty(t, written, "the fetched general.yaml never reached disk: %s", output)
}
