// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package root

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/metrics"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/reconcile"
)

func TestTickRecordsRunAndHealth(t *testing.T) {
	cfg, err := config.Load("../../../pkg/siem/config/testdata/tenants.example.json")
	require.NoError(t, err)
	flags.timeout = 10 * time.Second
	rec := metrics.New()
	var out bytes.Buffer
	opts := reconcile.Options{Config: cfg, Kube: fake.NewClientset(), DryRun: true, Only: []string{reconcile.ComponentRetention}}

	tick(context.Background(), &out, rec, opts, true)

	assert.Contains(t, out.String(), "--- run at ")
	assert.Contains(t, out.String(), "dry run, nothing written")
	st := rec.Status()
	require.NotNil(t, st.LastRun)
	assert.True(t, st.LastRun.DryRun)
	assert.Positive(t, st.LastRun.Errors, "no indexer credentials in the fake cluster")
	require.NotNil(t, st.Health)
	assert.Len(t, st.Health.Indexers, 3)
	assert.False(t, st.Healthy)
}

func TestServeFlags(t *testing.T) {
	for name, def := range map[string]string{"interval": "0s", "metrics-addr": ":9090", "probe": "true"} {
		f := RootCmd.Flags().Lookup(name)
		require.NotNil(t, f, name)
		assert.Equal(t, def, f.DefValue, name)
	}
}
