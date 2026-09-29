// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	ns          = "security-operations"
	oidcName    = "oidc"
	pwKey       = "password"
	admin       = "admin"
	apiKey      = "API_KEY"
	tenantNS    = "wazuh-001"
	irisKeyName = "iris-api-key"
)

func writes(kube *fake.Clientset) int {
	n := 0
	for _, a := range kube.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			n++
		}
	}
	return n
}

func specs() []config.GeneratedSecret {
	return []config.GeneratedSecret{{
		Namespace: ns,
		Name:      oidcName,
		Keys: []config.GeneratedKey{
			{Key: pwKey, Generator: config.GeneratorPassword},
			{Key: "hex", Generator: config.GeneratorHex32},
			{Key: "b64", Generator: config.GeneratorBase64},
			{Key: "client_id", Generator: config.GeneratorPassword, Value: "wazuh-dashboard-001"},
		},
	}}
}

func TestEnsureCreatesOnceAndNeverOverwrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset()

	res := Ensure(ctx, kube, specs(), true)
	require.Len(t, res, 1)
	assert.Equal(t, report.ActionCreate, res[0].Action)
	assert.Equal(t, 0, writes(kube), "dry run must not write")

	res = Ensure(ctx, kube, specs(), false)
	assert.Equal(t, report.ActionCreate, res[0].Action)
	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, oidcName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Len(t, sec.Data[pwKey], 32)
	assert.Len(t, sec.Data["hex"], 64)
	assert.NotEmpty(t, sec.Data["b64"])
	assert.Equal(t, "wazuh-dashboard-001", string(sec.Data["client_id"]), "a literal value is used as is")
	first := string(sec.Data[pwKey])

	before := writes(kube)
	res = Ensure(ctx, kube, specs(), false)
	assert.Equal(t, report.ActionOK, res[0].Action)
	assert.Equal(t, before, writes(kube), "second run must not write")
	sec, err = kube.CoreV1().Secrets(ns).Get(ctx, oidcName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, first, string(sec.Data[pwKey]))
}

func TestEnsureAddsMissingKeysOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: oidcName, Namespace: ns},
		Data:       map[string][]byte{pwKey: []byte("keep-me")},
	})
	res := Ensure(ctx, kube, specs(), true)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	assert.Equal(t, "missing keys b64,client_id,hex", res[0].Detail)

	res = Ensure(ctx, kube, specs(), false)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, oidcName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "keep-me", string(sec.Data[pwKey]))
	assert.NotEmpty(t, sec.Data["hex"])
}

func TestEnsureRefusesSealedSecret(t *testing.T) {
	t.Parallel()
	kube := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: oidcName, Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{{Kind: "SealedSecret", Name: oidcName}},
		},
		Data: map[string][]byte{pwKey: []byte("x")},
	})
	res := Ensure(context.Background(), kube, specs(), false)
	assert.Equal(t, report.ActionError, res[0].Action)
	assert.Contains(t, res[0].Detail, "SealedSecret")
	assert.Equal(t, 0, writes(kube))
}

func TestRead(t *testing.T) {
	t.Parallel()
	kube := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: admin, Namespace: "kc"},
		Data:       map[string][]byte{"pw": []byte("value\n")},
	})
	s := Store{Kube: kube}
	v, err := s.Read(context.Background(), config.SecretRef{Namespace: "kc", Name: admin, Key: "pw"})
	require.NoError(t, err)
	assert.Equal(t, "value", v, "trailing newline is trimmed")

	_, err = s.Read(context.Background(), config.SecretRef{Namespace: "kc", Name: admin, Key: "nope"})
	require.Error(t, err)
	_, err = s.Read(context.Background(), config.SecretRef{Namespace: "kc", Name: "missing", Key: "pw"})
	require.Error(t, err)
}

func TestPublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset()

	a, err := Publish(ctx, kube, ns, "velociraptor-api-client", "api_client.yaml", []byte("v1"))
	require.NoError(t, err)
	assert.Equal(t, report.ActionCreate, a)
	a, err = Publish(ctx, kube, ns, "velociraptor-api-client", "api_client.yaml", []byte("v1"))
	require.NoError(t, err)
	assert.Equal(t, report.ActionOK, a)
	a, err = Publish(ctx, kube, ns, "velociraptor-api-client", "api_client.yaml", []byte("v2"))
	require.NoError(t, err)
	assert.Equal(t, report.ActionUpdate, a)
}

func TestApply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset()
	want := map[string][]byte{"a": []byte("1"), "b": []byte("2")}

	a, _, err := Apply(ctx, kube, ns, "bundle", want, true)
	require.NoError(t, err)
	assert.Equal(t, report.ActionCreate, a)
	assert.Equal(t, 0, writes(kube), "dry run must not write")

	a, _, err = Apply(ctx, kube, ns, "bundle", want, false)
	require.NoError(t, err)
	assert.Equal(t, report.ActionCreate, a)
	a, _, err = Apply(ctx, kube, ns, "bundle", want, false)
	require.NoError(t, err)
	assert.Equal(t, report.ActionOK, a)

	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, "bundle", metav1.GetOptions{})
	require.NoError(t, err)
	sec.Data["b"] = []byte("drift")
	sec.Data["extra"] = []byte("kept")
	_, err = kube.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{})
	require.NoError(t, err)

	before := writes(kube)
	a, changed, err := Apply(ctx, kube, ns, "bundle", want, true)
	require.NoError(t, err)
	assert.Equal(t, report.ActionUpdate, a)
	assert.Equal(t, []string{"b"}, changed)
	assert.Equal(t, before, writes(kube), "dry run must not write")

	a, _, err = Apply(ctx, kube, ns, "bundle", want, false)
	require.NoError(t, err)
	assert.Equal(t, report.ActionUpdate, a)
	sec, err = kube.CoreV1().Secrets(ns).Get(ctx, "bundle", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "2", string(sec.Data["b"]))
	assert.Equal(t, "kept", string(sec.Data["extra"]), "other keys are kept")
}

func TestCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := config.SecretRef{Namespace: ns, Name: irisKeyName, Key: apiKey}
	dst := config.SecretRef{Namespace: tenantNS, Name: irisKeyName, Key: apiKey}
	gen := config.SecretRef{Namespace: ns, Name: oidcName, Key: pwKey}
	missing := config.SecretRef{Namespace: ns, Name: "absent", Key: "k"}
	copies := []config.SecretCopy{
		{From: src, To: dst},
		{From: gen, To: config.SecretRef{Namespace: tenantNS, Name: "oidc", Key: pwKey}},
		{From: missing, To: config.SecretRef{Namespace: tenantNS, Name: "other", Key: "k"}},
	}
	kube := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: irisKeyName, Namespace: ns},
		Data:       map[string][]byte{apiKey: []byte("key-1\n")},
	})
	actions := func(res []report.Result) []report.Action {
		out := make([]report.Action, 0, len(res))
		for _, r := range res {
			out = append(out, r.Action)
		}
		return out
	}

	res := Copy(ctx, kube, copies, specs(), true)
	assert.Equal(t, []report.Action{report.ActionCreate, report.ActionCreate, report.ActionError}, actions(res))
	assert.Contains(t, res[1].Detail, "generated in this run")
	assert.Equal(t, 0, writes(kube), "dry run must not write")

	res = Copy(ctx, kube, copies, specs(), false)
	assert.Equal(t, []report.Action{report.ActionCreate, report.ActionError, report.ActionError}, actions(res),
		"without the generated source an apply run cannot copy it")
	sec, err := kube.CoreV1().Secrets(tenantNS).Get(ctx, irisKeyName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "key-1\n", string(sec.Data[apiKey]), "bytes are copied unchanged")

	res = Copy(ctx, kube, copies[:1], nil, false)
	assert.Equal(t, report.ActionOK, res[0].Action)

	cur, err := kube.CoreV1().Secrets(ns).Get(ctx, irisKeyName, metav1.GetOptions{})
	require.NoError(t, err)
	cur.Data[apiKey] = []byte("key-2")
	_, err = kube.CoreV1().Secrets(ns).Update(ctx, cur, metav1.UpdateOptions{})
	require.NoError(t, err)
	res = Copy(ctx, kube, copies[:1], nil, false)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	assert.NotContains(t, res[0].Detail, "key-2", "values are never reported")
	sec, err = kube.CoreV1().Secrets(tenantNS).Get(ctx, irisKeyName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "key-2", string(sec.Data[apiKey]))
}
