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
	ns       = "security-operations"
	oidcName = "oidc"
	pwKey    = "password"
	admin    = "admin"
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
	assert.Equal(t, "missing keys b64,hex", res[0].Detail)

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
