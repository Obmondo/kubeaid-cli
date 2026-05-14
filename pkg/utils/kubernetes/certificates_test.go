// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// certWithStatus builds an unstructured cert-manager Certificate whose
// .status is the given map.
func certWithStatus(status map[string]any) *unstructured.Unstructured {
	cert := &unstructured.Unstructured{}
	cert.SetGroupVersionKind(certManagerCertificateGVK)
	cert.Object["status"] = status
	return cert
}

// certGetInterceptor returns a fake-client Get interceptor that serves
// getObj for any Certificate Get (or a NotFound when getObj is nil, or
// getErr when set). The Certificate is read via the unstructured
// client, so the interceptor short-circuits before the scheme/tracker.
func certGetInterceptor(getObj *unstructured.Unstructured, getErr error) interceptor.Funcs {
	return interceptor.Funcs{
		Get: func(
			_ context.Context, _ client.WithWatch, key client.ObjectKey,
			obj client.Object, _ ...client.GetOption,
		) error {
			switch {
			case getErr != nil:
				return getErr
			case getObj == nil:
				return k8sAPIErrors.NewNotFound(
					schema.GroupResource{Group: "cert-manager.io", Resource: "certificates"},
					key.Name,
				)
			default:
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return errors.New("certGetInterceptor: expected *unstructured.Unstructured")
				}
				u.Object = getObj.Object
				return nil
			}
		},
	}
}

func TestIsCertificateReady(t *testing.T) {
	t.Parallel()

	certKey := types.NamespacedName{Namespace: "keycloakx", Name: "keycloak-tls"}

	tests := []struct {
		name       string
		getObj     *unstructured.Unstructured
		getErr     error
		wantReady  bool
		wantDetail string // substring expected in the detail
	}{
		{
			name: "Ready=True returns ready",
			getObj: certWithStatus(map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "True"},
				},
			}),
			wantReady: true,
		},
		{
			name:       "missing Certificate returns not-created-yet",
			getObj:     nil,
			wantReady:  false,
			wantDetail: "not created yet",
		},
		{
			name:       "read error is surfaced",
			getErr:     errors.New("connection refused"),
			wantReady:  false,
			wantDetail: "read error",
		},
		{
			name: "no status conditions yet",
			getObj: func() *unstructured.Unstructured {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(certManagerCertificateGVK)
				return u
			}(),
			wantReady:  false,
			wantDetail: "no status conditions",
		},
		{
			// The load-bearing case: a stuck cert carries an unhelpful
			// Ready=False/DoesNotExist plus an Issuing=False/Failed that
			// holds the real reason — we must surface the latter.
			name: "Issuing=Failed surfaces the Issuing reason+message and attempt count",
			getObj: certWithStatus(map[string]any{
				"failedIssuanceAttempts": int64(2),
				"conditions": []any{
					map[string]any{
						"type": "Ready", "status": "False",
						"reason":  "DoesNotExist",
						"message": "Issuing certificate as Secret does not exist",
					},
					map[string]any{
						"type": "Issuing", "status": "False",
						"reason":  "Failed",
						"message": "order is in invalid state",
					},
				},
			}),
			wantReady:  false,
			wantDetail: "Failed: order is in invalid state [failedIssuanceAttempts=2]",
		},
		{
			name: "Ready=False without an Issuing-Failed falls back to the Ready message",
			getObj: certWithStatus(map[string]any{
				"conditions": []any{
					map[string]any{
						"type": "Ready", "status": "False",
						"reason":  "DoesNotExist",
						"message": "Issuing certificate as Secret does not exist",
					},
				},
			}),
			wantReady:  false,
			wantDetail: "DoesNotExist: Issuing certificate as Secret does not exist",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeClient := crFake.NewClientBuilder().
				WithScheme(newTestScheme(t)).
				WithInterceptorFuncs(certGetInterceptor(tc.getObj, tc.getErr)).
				Build()

			ready, detail := isCertificateReady(context.Background(), fakeClient, certKey)
			assert.Equal(t, tc.wantReady, ready)
			if tc.wantDetail != "" {
				assert.Contains(t, detail, tc.wantDetail)
			}
		})
	}
}

func TestWaitForCertificatesReady(t *testing.T) {
	// Shrink the loop intervals so the timeout path runs sub-second.
	// Mutates package-level vars — not t.Parallel().
	origTimeout, origPoll := waitForCertificatesReadyTimeout, waitForCertificatesReadyPollInterval
	t.Cleanup(func() {
		waitForCertificatesReadyTimeout = origTimeout
		waitForCertificatesReadyPollInterval = origPoll
	})
	waitForCertificatesReadyTimeout = 300 * time.Millisecond
	waitForCertificatesReadyPollInterval = 20 * time.Millisecond

	certKey := types.NamespacedName{Namespace: "netbird", Name: "netbird-tls"}

	t.Run("empty cert list is a no-op", func(t *testing.T) {
		require.NoError(t, WaitForCertificatesReady(context.Background(), nil, nil))
	})

	t.Run("returns nil once the Certificate is Ready", func(t *testing.T) {
		ready := certWithStatus(map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		})
		fakeClient := crFake.NewClientBuilder().
			WithScheme(newTestScheme(t)).
			WithInterceptorFuncs(certGetInterceptor(ready, nil)).
			Build()

		require.NoError(t, WaitForCertificatesReady(
			context.Background(), fakeClient, []types.NamespacedName{certKey},
		))
	})

	t.Run("times out with the failure reason when the Certificate never becomes Ready", func(t *testing.T) {
		stuck := certWithStatus(map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False", "reason": "DoesNotExist"},
				map[string]any{
					"type": "Issuing", "status": "False", "reason": "Failed",
					"message": "order is in invalid state",
				},
			},
		})
		fakeClient := crFake.NewClientBuilder().
			WithScheme(newTestScheme(t)).
			WithInterceptorFuncs(certGetInterceptor(stuck, nil)).
			Build()

		err := WaitForCertificatesReady(
			context.Background(), fakeClient, []types.NamespacedName{certKey},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")
		assert.Contains(t, err.Error(), "order is in invalid state")
	})
}
