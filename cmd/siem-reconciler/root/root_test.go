// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package root

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

// The commands share package-level flag state, so these tests run
// sequentially.

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	RootCmd.SetOut(&out)
	RootCmd.SetErr(&out)
	RootCmd.SetArgs(args)
	err := RootCmd.Execute()
	return out.String(), err
}

func TestVersion(t *testing.T) {
	out, err := execute(t, "version")
	require.NoError(t, err)
	assert.Contains(t, out, "version: dev")
}

func TestRejectsUnknownComponent(t *testing.T) {
	_, err := execute(t, "--only", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown component "nope"`)
}

func TestRejectsBadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"domain": ""}`), 0o600))
	_, err := execute(t, "--only", "", "--config", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain is required")
}

func TestPublishValidatesInput(t *testing.T) {
	_, err := execute(t, "publish-api-client", "--namespace", "ns")
	require.Error(t, err)

	path := filepath.Join(t.TempDir(), "api.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: x\n"), 0o600))
	_, err = execute(t, "publish-api-client", "--namespace", "ns", "--file", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ca_certificate")
}

func TestJoinComponents(t *testing.T) {
	assert.Equal(t, "secrets,enrolment,keycloak,iris,wazuh,wazuhcentral,velociraptor", joinComponents())
}

func TestResultError(t *testing.T) {
	ok := []report.Result{{Action: report.ActionOK}, {Action: report.ActionCreate}}
	failed := append(ok, report.Result{Action: report.ActionError}, report.Result{Action: report.ActionError})

	var log bytes.Buffer
	require.NoError(t, resultError(&log, ok, false))
	require.NoError(t, resultError(&log, ok, true))
	assert.Empty(t, log.String())

	err := resultError(&log, failed, false)
	require.Error(t, err)
	assert.Equal(t, "2 objects could not be reconciled", err.Error())
	assert.Empty(t, log.String())

	require.NoError(t, resultError(&log, failed, true))
	assert.Contains(t, log.String(), "2 objects could not be reconciled (ignored: --exit-zero)")
}

func TestExitZeroFlag(t *testing.T) {
	f := RootCmd.Flags().Lookup("exit-zero")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}
