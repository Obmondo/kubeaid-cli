// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// newFakeClient returns a controller-runtime fake client with apps/v1
// and core/v1 schemes registered — enough for the SealedSecrets-key
// helpers we exercise here.
func newFakeClient(t *testing.T, initObjs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(scheme))
	require.NoError(t, appsV1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjs...).Build()
}

func makeSealedSecretsKey(name string, data map[string][]byte) *coreV1.Secret {
	return &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: constants.NamespaceSealedSecrets,
			Labels: map[string]string{
				sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue,
			},
		},
		Type: coreV1.SecretTypeTLS,
		Data: data,
	}
}

func TestCopySealedSecretsKeysFromManagement_NoKeys(t *testing.T) {
	t.Parallel()

	mgmt := newFakeClient(t)
	main := newFakeClient(t)

	require.NoError(t, CopySealedSecretsKeysFromManagement(context.Background(), mgmt, main))

	// Nothing should have landed on main.
	var keys coreV1.SecretList
	require.NoError(t, main.List(context.Background(), &keys,
		client.InNamespace(constants.NamespaceSealedSecrets),
	))
	assert.Empty(t, keys.Items)
}

func TestCopySealedSecretsKeysFromManagement_SingleKeyCreated(t *testing.T) {
	t.Parallel()

	mgmtKey := makeSealedSecretsKey("sealed-secrets-keyabc12", map[string][]byte{
		"tls.crt": []byte("cert-bytes"),
		"tls.key": []byte("key-bytes"),
	})
	mgmt := newFakeClient(t, mgmtKey)
	main := newFakeClient(t)

	require.NoError(t, CopySealedSecretsKeysFromManagement(context.Background(), mgmt, main))

	got := &coreV1.Secret{}
	require.NoError(t, main.Get(context.Background(),
		types.NamespacedName{Namespace: constants.NamespaceSealedSecrets, Name: mgmtKey.Name},
		got,
	))
	assert.Equal(t, mgmtKey.Data, got.Data)
	assert.Equal(t, sealedSecretsActiveKeyValue, got.Labels[sealedSecretsActiveKeyLabel])
	assert.Equal(t, coreV1.SecretTypeTLS, got.Type)
}

func TestCopySealedSecretsKeysFromManagement_MultipleKeysCopiedIndependently(t *testing.T) {
	t.Parallel()

	k1 := makeSealedSecretsKey("sealed-secrets-keya", map[string][]byte{"tls.key": []byte("a")})
	k2 := makeSealedSecretsKey("sealed-secrets-keyb", map[string][]byte{"tls.key": []byte("b")})
	mgmt := newFakeClient(t, k1, k2)
	main := newFakeClient(t)

	require.NoError(t, CopySealedSecretsKeysFromManagement(context.Background(), mgmt, main))

	var keys coreV1.SecretList
	require.NoError(t, main.List(context.Background(), &keys,
		client.InNamespace(constants.NamespaceSealedSecrets),
		client.MatchingLabels{sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue},
	))
	assert.Len(t, keys.Items, 2)
}

func TestCopySealedSecretsKeysFromManagement_RefreshesAlreadyPresent(t *testing.T) {
	t.Parallel()

	const keyName = "sealed-secrets-keyabc12"
	mgmtKey := makeSealedSecretsKey(keyName, map[string][]byte{
		"tls.key": []byte("fresh-from-mgmt"),
	})
	// main already has a Secret with the same name but stale data —
	// simulates a re-bootstrap on top of an existing main cluster.
	stale := makeSealedSecretsKey(keyName, map[string][]byte{
		"tls.key": []byte("stale-data"),
	})

	mgmt := newFakeClient(t, mgmtKey)
	main := newFakeClient(t, stale)

	require.NoError(t, CopySealedSecretsKeysFromManagement(context.Background(), mgmt, main))

	got := &coreV1.Secret{}
	require.NoError(t, main.Get(context.Background(),
		types.NamespacedName{Namespace: constants.NamespaceSealedSecrets, Name: keyName},
		got,
	))
	assert.Equal(t, []byte("fresh-from-mgmt"), got.Data["tls.key"],
		"existing key should be refreshed with the management cluster's current bytes")
}

func TestCopySealedSecretsKeysFromManagement_OnlyActiveLabelMatches(t *testing.T) {
	t.Parallel()

	active := makeSealedSecretsKey("sealed-secrets-keyactive", map[string][]byte{"tls.key": []byte("a")})
	unrelated := &coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "some-other-secret",
			Namespace: constants.NamespaceSealedSecrets,
			// No sealed-secrets-key label — must NOT be copied.
		},
		Data: map[string][]byte{"unrelated": []byte("x")},
	}

	mgmt := newFakeClient(t, active, unrelated)
	main := newFakeClient(t)

	require.NoError(t, CopySealedSecretsKeysFromManagement(context.Background(), mgmt, main))

	// The unrelated Secret must NOT have landed on main.
	err := main.Get(context.Background(),
		types.NamespacedName{Namespace: constants.NamespaceSealedSecrets, Name: unrelated.Name},
		&coreV1.Secret{},
	)
	assert.Error(t, err, "unrelated Secret without the sealed-secrets-key=active label must not be copied")
}

func TestWaitForSealedSecretsControllerReady_AlreadyReady(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsV1.Deployment{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      sealedSecretsControllerDeploymentName,
			Namespace: constants.NamespaceSealedSecrets,
		},
		Spec: appsV1.DeploymentSpec{Replicas: &replicas},
		Status: appsV1.DeploymentStatus{
			Replicas:          1,
			AvailableReplicas: 1,
		},
	}
	c := newFakeClient(t, dep)

	require.NoError(t, WaitForSealedSecretsControllerReady(context.Background(), c, 5*time.Second))
}

func TestWaitForSealedSecretsControllerReady_TimesOutWhenNotReady(t *testing.T) {
	t.Parallel()

	replicas := int32(1)
	dep := &appsV1.Deployment{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      sealedSecretsControllerDeploymentName,
			Namespace: constants.NamespaceSealedSecrets,
		},
		Spec: appsV1.DeploymentSpec{Replicas: &replicas},
		Status: appsV1.DeploymentStatus{
			Replicas:          1,
			AvailableReplicas: 0, // pod not yet healthy
		},
	}
	c := newFakeClient(t, dep)

	err := WaitForSealedSecretsControllerReady(context.Background(), c, 1*time.Second)
	require.Error(t, err, "should time out when AvailableReplicas < desired")
}

func TestWaitForSealedSecretsControllerReady_TimesOutWhenDeploymentMissing(t *testing.T) {
	t.Parallel()

	c := newFakeClient(t) // no deployment at all

	err := WaitForSealedSecretsControllerReady(context.Background(), c, 1*time.Second)
	require.Error(t, err, "should time out when the Deployment hasn't been created yet")
}
