// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const irisComponent = "iris"

func TestPrintAndSummarize(t *testing.T) {
	t.Parallel()
	results := []Result{
		{Component: "keycloak", Kind: "group", Name: "tenant-001", Action: ActionOK},
		{Component: "keycloak", Kind: "client", Name: "iris", Action: ActionUpdate, Detail: "redirectUris"},
		{Component: irisComponent, Kind: "customer", Name: "Tenant A", Action: ActionCreate},
		{Component: "wazuh", Kind: "api", Name: "login", Action: ActionError, Detail: "boom"},
		{Component: irisComponent, Kind: "service-account", Name: "svc", Action: ActionSkip},
	}
	assert.Equal(t, Summary{Changes: 2, Errors: 1}, Summarize(results))

	var buf bytes.Buffer
	require.NoError(t, Print(&buf, results, true))
	out := buf.String()
	assert.Contains(t, out, "COMPONENT")
	assert.Contains(t, out, "redirectUris")
	assert.Contains(t, out, "2 changes, 1 errors (dry run, nothing written)")

	buf.Reset()
	require.NoError(t, Print(&buf, nil, false))
	assert.Contains(t, buf.String(), "0 changes, 0 errors (applied)")
}
