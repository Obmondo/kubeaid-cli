// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package enrolment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	orgA      = "Tenant A"
	clientCfg = "version:\n  name: velociraptor\n  version: 0.77.1\nClient:\n  server_urls:\n  - https://velociraptor.example.com:8000/\n  nonce: bm9uY2UtYQ==\n"
)

func TestRenderVelociraptor(t *testing.T) {
	t.Parallel()
	data := RenderVelociraptor(bundle("001", ns), orgA, clientCfg)
	assert.Equal(t, clientCfg, string(data[KeyVelociraptorConfig]), "the server's config is copied as is")
	hint := string(data[KeyVelociraptorInstall])
	assert.Contains(t, hint, "tenant 001 (org Tenant A)")
	assert.Contains(t, hint, "https://github.com/Velocidex/velociraptor/releases/tag/v0.77.1")
	assert.Contains(t, hint, "debian client")
	assert.Contains(t, hint, "service install")

	hint = string(RenderVelociraptor(bundle("001", ns), orgA, "Client: {}\n")[KeyVelociraptorInstall])
	assert.Contains(t, hint, "https://github.com/Velocidex/velociraptor/releases\n", "no version: link the release list")
}

func TestEnsureVelociraptor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset(authd(ns, "pw-1"))
	require.Equal(t, report.ActionCreate, Ensure(ctx, kube, []config.EnrolmentBundle{bundle("001", ns)}, false)[0].Action)
	kube.ClearActions()

	vb := []VelociraptorBundle{{Bundle: bundle("001", ns), Org: orgA, ClientConfigs: []string{clientCfg}}}

	res := EnsureVelociraptor(ctx, kube, vb, true)
	require.Len(t, res, 1)
	assert.Equal(t, report.Result{
		Component: "velociraptor", Kind: KindBundle, Name: ns + "/" + bundleName,
		Action: report.ActionUpdate, Detail: "keys install-velociraptor.txt,velociraptor-client.config.yaml",
	}, res[0])
	for _, a := range kube.Actions() {
		assert.Equal(t, "get", a.GetVerb(), "dry run only reads")
	}

	res = EnsureVelociraptor(ctx, kube, vb, false)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, bundleName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, clientCfg, string(sec.Data[KeyVelociraptorConfig]))
	assert.Equal(t, "pw-1", string(sec.Data[KeyAuthdPass]), "Wazuh keys are kept")
	assert.Len(t, sec.Data, 9)

	kube.ClearActions()
	res = EnsureVelociraptor(ctx, kube, vb, false)
	assert.Equal(t, report.ActionOK, res[0].Action, "second run is a no-op")
	for _, a := range kube.Actions() {
		assert.Equal(t, "get", a.GetVerb(), "no write when nothing differs")
	}
	assert.Equal(t, report.ActionOK, Ensure(ctx, kube, []config.EnrolmentBundle{bundle("001", ns)}, false)[0].Action,
		"the enrolment step leaves the Velociraptor keys alone")

	vb[0].ClientConfigs = []string{clientCfg + "  # rotated\n"}
	res = EnsureVelociraptor(ctx, kube, vb, false)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	assert.Equal(t, "keys velociraptor-client.config.yaml", res[0].Detail)
}

func TestEnsureVelociraptorMissingOrAmbiguousOrg(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset()
	b := bundle("001", ns)

	res := EnsureVelociraptor(ctx, kube, []VelociraptorBundle{{Bundle: b, Org: orgA}}, true)
	assert.Equal(t, report.ActionCreate, res[0].Action, "dry run: the org is created in this run")
	res = EnsureVelociraptor(ctx, kube, []VelociraptorBundle{{Bundle: b, Org: orgA}}, false)
	assert.Equal(t, report.ActionError, res[0].Action)
	res = EnsureVelociraptor(ctx, kube, []VelociraptorBundle{{Bundle: b, Org: orgA, ClientConfigs: []string{"a", "b"}}}, false)
	assert.Equal(t, report.ActionError, res[0].Action)
	assert.Empty(t, kube.Actions(), "nothing is read or written without exactly one org")

	res = EnsureVelociraptor(ctx, kube, []VelociraptorBundle{{Bundle: b, Org: orgA, ClientConfigs: []string{clientCfg}}}, true)
	assert.Equal(t, report.ActionCreate, res[0].Action, "missing bundle")
	res = EnsureVelociraptor(ctx, kube, []VelociraptorBundle{{Bundle: b, Org: orgA, ClientConfigs: []string{clientCfg}}}, false)
	assert.Equal(t, report.ActionCreate, res[0].Action)
	assert.Equal(t, "org Tenant A", res[0].Detail)
}
