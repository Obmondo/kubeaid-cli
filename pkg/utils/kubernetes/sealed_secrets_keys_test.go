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

// healthyDeployment returns a Deployment manifest matching the
// fully-rolled-out condition deploymentFullyAvailable accepts.
// Centralised so test bodies stay focused on the property each
// case is exercising.
func healthyDeployment() *appsV1.Deployment {
	replicas := int32(1)
	return &appsV1.Deployment{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      sealedSecretsControllerDeploymentName,
			Namespace: constants.NamespaceSealedSecrets,
			Generation: 1,
		},
		Spec: appsV1.DeploymentSpec{Replicas: &replicas},
		Status: appsV1.DeploymentStatus{
			ObservedGeneration:  1,
			Replicas:            1,
			AvailableReplicas:   1,
			ReadyReplicas:       1,
			UnavailableReplicas: 0,
		},
	}
}

func TestWaitForControllerHealthy_AlreadyHealthy(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t, healthyDeployment())
	require.NoError(t, waitForControllerHealthy(context.Background(), c, 5*time.Second))
}

func TestWaitForControllerHealthy_TimesOutOnUnavailableReplicas(t *testing.T) {
	t.Parallel()
	dep := healthyDeployment()
	dep.Status.AvailableReplicas = 0
	c := newFakeClient(t, dep)

	err := waitForControllerHealthy(context.Background(), c, 1*time.Second)
	require.Error(t, err, "should time out when AvailableReplicas < desired")
}

func TestWaitForControllerHealthy_ReturnsImmediatelyWhenDeploymentMissing(t *testing.T) {
	t.Parallel()
	c := newFakeClient(t) // no deployment at all

	start := time.Now()
	// Use a generous timeout to prove the function returns FAST,
	// not at the timeout — Deployment-not-found is a definitive
	// failure (only Helm creates the Deployment), not a transient
	// state to keep polling on.
	err := waitForControllerHealthy(context.Background(), c, 30*time.Second)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, errDeploymentMissing,
		"missing Deployment should surface errDeploymentMissing, not a timeout")
	assert.Less(t, elapsed, 3*time.Second,
		"should NOT wait the full 30s — Deployment-missing is a fast-fail signal")
}

func TestWaitForControllerHealthy_TimesOutOnGenerationDrift(t *testing.T) {
	t.Parallel()
	dep := healthyDeployment()
	dep.Generation = 3
	dep.Status.ObservedGeneration = 1 // controller hasn't observed latest spec
	c := newFakeClient(t, dep)

	err := waitForControllerHealthy(context.Background(), c, 1*time.Second)
	require.Error(t, err, "should time out when ObservedGeneration is behind Generation")
}

// withReinstallStub temporarily replaces reinstallSealedSecretsFn so
// EnsureSealedSecretsHealthy can be exercised without running an
// actual Helm install.
func withReinstallStub(t *testing.T, stub func(ctx context.Context) error) {
	t.Helper()
	orig := reinstallSealedSecretsFn
	reinstallSealedSecretsFn = stub
	t.Cleanup(func() { reinstallSealedSecretsFn = orig })
}

// EnsureSealedSecretsHealthy tests mutate reinstallSealedSecretsFn
// (a package-level var) and healthPollTimeoutForTest — they can't run
// in parallel against each other.

func TestEnsureSealedSecretsHealthy_HappyPath(t *testing.T) {

	mgmtKey := makeSealedSecretsKey("sealed-secrets-keyabc", map[string][]byte{"tls.key": []byte("a")})
	mainKey := makeSealedSecretsKey("sealed-secrets-keyabc", map[string][]byte{"tls.key": []byte("a")})
	dep := healthyDeployment()

	mgmt := newFakeClient(t, mgmtKey)
	main := newFakeClient(t, mainKey, dep)

	withReinstallStub(t, func(_ context.Context) error {
		t.Fatal("reinstall should not be called on the happy path")
		return nil
	})

	require.NoError(t, EnsureSealedSecretsHealthy(context.Background(), mgmt, main))
}

func TestEnsureSealedSecretsHealthy_KeyMismatch_TriggersRecopy(t *testing.T) {

	mgmtKey1 := makeSealedSecretsKey("sealed-secrets-keyaaa", map[string][]byte{"tls.key": []byte("a")})
	mgmtKey2 := makeSealedSecretsKey("sealed-secrets-keybbb", map[string][]byte{"tls.key": []byte("b")})
	// main has only one of the two keys initially.
	mainKey1 := makeSealedSecretsKey("sealed-secrets-keyaaa", map[string][]byte{"tls.key": []byte("a")})
	dep := healthyDeployment()

	mgmt := newFakeClient(t, mgmtKey1, mgmtKey2)
	main := newFakeClient(t, mainKey1, dep)

	withReinstallStub(t, func(_ context.Context) error {
		t.Fatal("reinstall should not be called when only keys are mismatched")
		return nil
	})

	require.NoError(t, EnsureSealedSecretsHealthy(context.Background(), mgmt, main))

	// After recovery, main must have both keys.
	var allMain coreV1.SecretList
	require.NoError(t, main.List(context.Background(), &allMain,
		client.InNamespace(constants.NamespaceSealedSecrets),
		client.MatchingLabels{sealedSecretsActiveKeyLabel: sealedSecretsActiveKeyValue},
	))
	assert.Len(t, allMain.Items, 2, "missing key should have been copied during recovery")
}

func TestEnsureSealedSecretsHealthy_UnhealthyDeployment_TriggersReinstall(t *testing.T) {

	mgmtKey := makeSealedSecretsKey("sealed-secrets-keyabc", map[string][]byte{"tls.key": []byte("a")})
	mainKey := makeSealedSecretsKey("sealed-secrets-keyabc", map[string][]byte{"tls.key": []byte("a")})

	// Initially unhealthy Deployment.
	unhealthy := healthyDeployment()
	unhealthy.Status.AvailableReplicas = 0

	mgmt := newFakeClient(t, mgmtKey)
	main := newFakeClient(t, mainKey, unhealthy)

	reinstallCalled := 0
	withReinstallStub(t, func(_ context.Context) error {
		reinstallCalled++
		// Simulate a successful reinstall by patching the Deployment to healthy.
		var dep appsV1.Deployment
		_ = main.Get(context.Background(), types.NamespacedName{
			Namespace: constants.NamespaceSealedSecrets,
			Name:      sealedSecretsControllerDeploymentName,
		}, &dep)
		dep.Status.AvailableReplicas = 1
		dep.Status.ReadyReplicas = 1
		dep.Status.UnavailableReplicas = 0
		dep.Status.ObservedGeneration = dep.Generation
		_ = main.Status().Update(context.Background(), &dep)
		return nil
	})

	// Use a short health-check budget so the first poll fails quickly.
	origTimeout := healthPollTimeoutForTest
	healthPollTimeoutForTest = 500 * time.Millisecond
	t.Cleanup(func() { healthPollTimeoutForTest = origTimeout })

	require.NoError(t, EnsureSealedSecretsHealthy(context.Background(), mgmt, main))
	assert.Equal(t, 1, reinstallCalled, "reinstall should have been called exactly once")
}

func TestEnsureSealedSecretsHealthy_ReinstallFailsToFix_ReturnsDiagnostic(t *testing.T) {

	mgmtKey := makeSealedSecretsKey("sealed-secrets-keyabc", map[string][]byte{"tls.key": []byte("a")})
	mainKey := makeSealedSecretsKey("sealed-secrets-keyabc", map[string][]byte{"tls.key": []byte("a")})

	// No Deployment at all → reinstall stub doesn't create one →
	// EnsureSealedSecretsHealthy should return the rich diagnostic.
	mgmt := newFakeClient(t, mgmtKey)
	main := newFakeClient(t, mainKey)

	withReinstallStub(t, func(_ context.Context) error {
		return nil // reinstall "succeeds" but doesn't actually fix anything
	})

	origTimeout := healthPollTimeoutForTest
	healthPollTimeoutForTest = 200 * time.Millisecond
	t.Cleanup(func() { healthPollTimeoutForTest = origTimeout })

	err := EnsureSealedSecretsHealthy(context.Background(), mgmt, main)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not Ready after reinstall")
	assert.Contains(t, err.Error(), "NOT FOUND",
		"rich diagnostic should call out the missing Deployment")
}
