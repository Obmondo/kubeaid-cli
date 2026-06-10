// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	caphV1Beta1 "github.com/syself/cluster-api-provider-hetzner/api/v1beta1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeadmConstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"
	"k8s.io/utils/ptr"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

const (
	testClusterName          = "test-cluster"
	testCapiClusterNamespace = "capi-cluster"
)

// newClusterAPITestScheme builds a scheme that includes coreV1,
// cluster-api types, and CAPH types. CAPH is included so summarizeCAPIStatus
// tests can cover the Hetzner-only overlay path that reads HBMM
// status directly to bypass the Machine controller's lagged copy of
// InfrastructureRef status. Non-Hetzner test cases simply don't seed
// HBMM objects; the overlay then has nothing to read and the row falls
// through to the generic Machine-derived status.
func newClusterAPITestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	require.NoError(t, clusterAPIV1Beta1.AddToScheme(s))
	require.NoError(t, caphV1Beta1.AddToScheme(s))
	return s
}

func TestUsingClusterAPI(t *testing.T) {
	tests := []struct {
		name          string
		cloudProvider string
		want          bool
	}{
		{
			name:          "BareMetal returns false",
			cloudProvider: constants.CloudProviderBareMetal,
			want:          false,
		},
		{
			name:          "Local returns false",
			cloudProvider: constants.CloudProviderLocal,
			want:          false,
		},
		{
			name:          "AWS returns true",
			cloudProvider: constants.CloudProviderAWS,
			want:          true,
		},
		{
			name:          "Azure returns true",
			cloudProvider: constants.CloudProviderAzure,
			want:          true,
		},
		{
			name:          "Hetzner returns true",
			cloudProvider: constants.CloudProviderHetzner,
			want:          true,
		},
	}

	// Mutates globals.CloudProviderName — sequential only.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := globals.CloudProviderName
			t.Cleanup(func() { globals.CloudProviderName = original })
			globals.CloudProviderName = tc.cloudProvider

			got := UsingClusterAPI()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetCapiClusterNamespace(t *testing.T) {
	// Result is constant — the customerid suffix was removed because the
	// management cluster is throwaway and each workload cluster's CAPI
	// is single-tenant. Test guards against accidental reintroduction.
	tests := []struct {
		name    string
		obmondo *config.ObmondoConfig
	}{
		{"no Obmondo config", nil},
		{"Obmondo config with empty CustomerID", &config.ObmondoConfig{CustomerID: ""}},
		{"Obmondo config with CustomerID", &config.ObmondoConfig{CustomerID: "acme"}},
		{"CustomerID with hyphens", &config.ObmondoConfig{CustomerID: "my-company-123"}},
	}

	// Mutates ParsedGeneralConfig — sequential only.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() { config.ParsedGeneralConfig.Obmondo = original })
			config.ParsedGeneralConfig.Obmondo = tc.obmondo

			assert.Equal(t, testCapiClusterNamespace, GetCapiClusterNamespace())
		})
	}
}

func TestGetClusterResource(t *testing.T) {
	// Mutates ParsedGeneralConfig.Cluster.Name and Obmondo — sequential only.
	scheme := newClusterAPITestScheme(t)

	tests := []struct {
		name      string
		preExist  []runtime.Object
		wantErr   bool
		wantPhase string
	}{
		{
			name: "returns cluster resource when it exists",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
					},
				},
			},
			wantErr:   false,
			wantPhase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
		},
		{
			name:     "returns error when cluster resource does not exist",
			preExist: []runtime.Object{},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// GetClusterResource reads config.ParsedGeneralConfig.Cluster.Name
			// and GetCapiClusterNamespace() (which reads Obmondo config).
			// We set these to fixed values for the test.
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() {
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
			})
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&clusterAPIV1Beta1.Cluster{}).
				WithRuntimeObjects(tc.preExist...).
				Build()

			got, err := GetClusterResource(context.Background(), fakeClient)

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPhase, got.Status.Phase)
		})
	}
}

func TestWaitForMainClusterToBeProvisioned(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	tests := []struct {
		name       string
		preExist   []runtime.Object
		ctxTimeout time.Duration
		wantErr    bool
	}{
		{
			name: "returns nil when cluster is provisioned and ready",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
						Conditions: clusterAPIV1Beta1.Conditions{
							{
								Type:   clusterAPIV1Beta1.ReadyCondition,
								Status: "True",
							},
						},
					},
				},
			},
			ctxTimeout: 5 * time.Second,
		},
		{
			name:       "returns error when cluster does not exist and context expires",
			preExist:   []runtime.Object{},
			ctxTimeout: 500 * time.Millisecond,
			wantErr:    true,
		},
		{
			name: "returns error when cluster is not provisioned and context expires",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhasePending),
					},
				},
			},
			ctxTimeout: 500 * time.Millisecond,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			origPoll := capiWaitPollInterval
			origTotal := capiWaitTotalTimeout
			t.Cleanup(func() {
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
				capiWaitPollInterval = origPoll
				capiWaitTotalTimeout = origTotal
			})
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil
			// Sub-second polling + tight total so the test exercises
			// both the success-on-first-tick path and the timeout path
			// without sleeping for minutes.
			capiWaitPollInterval = 50 * time.Millisecond
			capiWaitTotalTimeout = 2 * time.Second

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...).
				Build()

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			err := WaitForMainClusterToBeProvisioned(ctx, fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestSummarizeCAPIStatus exercises the helper directly so we can assert
// on the row content + ready flag without sitting through the wait loop.
// Three cases match the live-wait scenarios an operator hits — all
// driven by Cluster + Machine status alone (no provider-specific
// objects), since the Machine controller already aggregates infra-side
// status into the Machine's own v1beta2 conditions:
//   - happy path: cluster Provisioned + Ready, Machine Running → ready=true.
//   - in-progress: Machine Provisioning, v1beta2 Ready=False with a
//     bullet rollup → ready=false, Status surfaces the rollup's first line.
//   - failure: Machine v1beta2 InfrastructureReady=False with a placement
//     error → ready=false, row.Failed=true (Phase=Failed), Status carries
//     the error so the operator can abort/diagnose.
func TestSummarizeCAPIStatus(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	tests := []struct {
		name             string
		cloudProvider    string // "" leaves globals.CloudProviderName untouched; "hetzner" exercises HBMM overlay
		preExist         []runtime.Object
		wantReady        bool
		wantRowCount     int
		wantClusterPhase string
		wantFailedRow    bool
		wantStatusSubstr string // substring expected in some row's Status column
	}{
		{
			name: "happy path — cluster provisioned and ready, machine running",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
						Conditions: clusterAPIV1Beta1.Conditions{
							{Type: clusterAPIV1Beta1.ReadyCondition, Status: "True"},
						},
					},
				},
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "cp-1",
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseRunning),
						Conditions: clusterAPIV1Beta1.Conditions{
							{Type: clusterAPIV1Beta1.ReadyCondition, Status: "True"},
						},
					},
				},
			},
			wantReady:        true,
			wantRowCount:     2,
			wantClusterPhase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
		},
		{
			name: "in progress — machine v1beta2 Ready=False with bullet rollup",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioning),
						Conditions: clusterAPIV1Beta1.Conditions{
							{
								Type:   clusterAPIV1Beta1.ReadyCondition,
								Status: "False",
								Reason: "WaitingForControlPlaneInitialized",
							},
						},
					},
				},
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "cp-1",
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseProvisioning),
						V1Beta2: &clusterAPIV1Beta1.MachineV1Beta2Status{
							Conditions: []metaV1.Condition{
								// Available has empty Message → must be skipped.
								{Type: "Available", Status: metaV1.ConditionFalse, Reason: "NotReady"},
								// Ready carries the rollup with the actual error.
								{
									Type:    "Ready",
									Status:  metaV1.ConditionFalse,
									Reason:  "NotReady",
									Message: "* InfrastructureReady: Server is starting\n* NodeHealthy: Waiting for control plane",
								},
							},
						},
					},
				},
			},
			wantReady:        false,
			wantRowCount:     2,
			wantClusterPhase: string(clusterAPIV1Beta1.ClusterPhaseProvisioning),
			wantStatusSubstr: "InfrastructureReady: Server is starting",
		},
		{
			// Real-world Hetzner case: Machine is still Phase=Provisioning
			// because CAPH keeps retrying the transient placement error,
			// but the InfrastructureReady condition's Reason is
			// "ServerCreateFailedReason" — operators want this row red
			// even though Phase isn't "Failed".
			name: "failure — Machine Phase=Provisioning with ServerCreateFailedReason",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioning),
					},
				},
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "cp-broken",
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseProvisioning),
						V1Beta2: &clusterAPIV1Beta1.MachineV1Beta2Status{
							Conditions: []metaV1.Condition{
								{Type: "Available", Status: metaV1.ConditionFalse, Reason: "NotReady"},
								{
									Type:    "InfrastructureReady",
									Status:  metaV1.ConditionFalse,
									Reason:  "ServerCreateFailedReason",
									Message: "error during placement (resource_unavailable, abc123)",
								},
							},
						},
					},
				},
			},
			wantReady:        false,
			wantRowCount:     2,
			wantClusterPhase: string(clusterAPIV1Beta1.ClusterPhaseProvisioning),
			wantFailedRow:    true,
			wantStatusSubstr: "error during placement (resource_unavailable",
		},
		{
			// CAPH-specific overlay: the Machine row's Status must come
			// from HBMM's live Ready-condition message, NOT the stale
			// Machine.status copy. Real-world shape: CAPH bounced the
			// host registering → image-installing → back to registering
			// after a transient SSH glitch; HBMM message reflects the
			// current "registering" state, but the Machine controller
			// hasn't refreshed InfrastructureReady's message yet so
			// machineStatusDetail would return the stale
			// "image-installing" string. The overlay must win.
			name:          "hetzner — HBMM live message overlays stale Machine status",
			cloudProvider: constants.CloudProviderHetzner,
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
					},
				},
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "cp-g7nxx",
						Namespace: testCapiClusterNamespace,
					},
					Spec: clusterAPIV1Beta1.MachineSpec{
						InfrastructureRef: coreV1.ObjectReference{
							Kind: "HetznerBareMetalMachine",
							Name: "kbm-cp-g7nxx",
						},
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseProvisioning),
						V1Beta2: &clusterAPIV1Beta1.MachineV1Beta2Status{
							Conditions: []metaV1.Condition{
								{
									Type:    "Ready",
									Status:  metaV1.ConditionFalse,
									Reason:  "NotReady",
									Message: "* InfrastructureReady: host (1455733) is still provisioning - state \"image-installing\"",
								},
							},
						},
					},
				},
				&caphV1Beta1.HetznerBareMetalMachine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "kbm-cp-g7nxx",
						Namespace: testCapiClusterNamespace,
					},
					Status: caphV1Beta1.HetznerBareMetalMachineStatus{
						Conditions: clusterAPIV1Beta1.Conditions{
							{
								Type:    clusterAPIV1Beta1.ReadyCondition,
								Status:  coreV1.ConditionFalse,
								Reason:  "StillProvisioning",
								Message: "host (1455733) is still provisioning - state \"registering\"",
							},
						},
					},
				},
			},
			wantReady:        false,
			wantRowCount:     2,
			wantClusterPhase: string(clusterAPIV1Beta1.ClusterPhaseProvisioned),
			wantStatusSubstr: "state \"registering\"",
		},
		{
			// HBMM terminal failure (status.failureMessage set) wins
			// over both the Ready condition and the Machine row's
			// inferred message — the operator should see exactly what
			// CAPH considers fatal (e.g. CheckDisk permanent error)
			// without us paraphrasing.
			name:          "hetzner — HBMM failureMessage overrides everything else",
			cloudProvider: constants.CloudProviderHetzner,
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Cluster{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      testClusterName,
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.ClusterStatus{
						Phase: string(clusterAPIV1Beta1.ClusterPhaseProvisioning),
					},
				},
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "w-bad-disk",
						Namespace: testCapiClusterNamespace,
					},
					Spec: clusterAPIV1Beta1.MachineSpec{
						InfrastructureRef: coreV1.ObjectReference{
							Kind: "HetznerBareMetalMachine",
							Name: "kbm-w-bad-disk",
						},
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseProvisioning),
					},
				},
				&caphV1Beta1.HetznerBareMetalMachine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "kbm-w-bad-disk",
						Namespace: testCapiClusterNamespace,
					},
					Status: caphV1Beta1.HetznerBareMetalMachineStatus{
						FailureMessage: ptr.To("CheckDisk failed (permanent error): Airflow_Temperature_Cel in_the_past"),
					},
				},
			},
			wantReady:        false,
			wantRowCount:     2,
			wantClusterPhase: string(clusterAPIV1Beta1.ClusterPhaseProvisioning),
			wantStatusSubstr: "CheckDisk failed (permanent error)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() {
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
			})
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil

			if tc.cloudProvider != "" {
				origCloud := globals.CloudProviderName
				t.Cleanup(func() { globals.CloudProviderName = origCloud })
				globals.CloudProviderName = tc.cloudProvider
			}

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...).
				Build()

			rows, ready, err := summarizeCAPIStatus(context.Background(), fakeClient)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReady, ready)
			require.Len(t, rows, tc.wantRowCount)

			// First row is always the Cluster.
			assert.Equal(t, "Cluster/"+testClusterName, rows[0].Resource)
			assert.Equal(t, tc.wantClusterPhase, rows[0].Phase)

			if tc.wantFailedRow {
				foundFailed := false
				for _, r := range rows {
					if r.Failed {
						foundFailed = true
						break
					}
				}
				assert.True(t, foundFailed, "expected at least one row with Failed=true")
			}

			if tc.wantStatusSubstr != "" {
				found := false
				for _, r := range rows {
					if assert.ObjectsAreEqual(r.Status, tc.wantStatusSubstr) ||
						containsSubstr(r.Status, tc.wantStatusSubstr) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected some row.Status to contain %q; rows=%+v", tc.wantStatusSubstr, rows)
			}
		})
	}
}

// containsSubstr is a tiny helper to keep the assertion readable —
// strings.Contains is fine but pulling in the import for one call site
// inflates the test diff.
func containsSubstr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestSummarizeMachinesForPivot exercises the predicate WaitForAllMachinesRunning
// loops on. clusterctl move's pre-condition is "every Machine has a Node
// registered", which we model as Phase=Running AND status.nodeRef != nil.
// Cases cover the three states the operator can be in at pivot time:
//
//   - all Running with NodeRef       → ready=true
//   - rolling update mid-flight      → ready=false (the kk52w case)
//   - no Machines yet                → ready=false (don't pivot an empty ns)
func TestSummarizeMachinesForPivot(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	runningWithNode := func(name string) *clusterAPIV1Beta1.Machine {
		return &clusterAPIV1Beta1.Machine{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: testCapiClusterNamespace,
			},
			Status: clusterAPIV1Beta1.MachineStatus{
				Phase: string(clusterAPIV1Beta1.MachinePhaseRunning),
				NodeRef: &coreV1.ObjectReference{
					Kind: "Node",
					Name: name,
				},
			},
		}
	}
	provisionedNoNode := func(name string) *clusterAPIV1Beta1.Machine {
		return &clusterAPIV1Beta1.Machine{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: testCapiClusterNamespace,
			},
			Status: clusterAPIV1Beta1.MachineStatus{
				Phase: string(clusterAPIV1Beta1.MachinePhaseProvisioned),
			},
		}
	}

	tests := []struct {
		name         string
		preExist     []runtime.Object
		wantReady    bool
		wantRowCount int
	}{
		{
			name: "all Machines Running with NodeRef → ready",
			preExist: []runtime.Object{
				runningWithNode("cp-1"),
				runningWithNode("worker-1"),
			},
			wantReady:    true,
			wantRowCount: 2,
		},
		{
			// Real-world rolling-update case: the old control-plane Machine
			// is Running with its Node, the surge replacement is
			// Phase=Provisioned but the Node hasn't joined yet (no nodeRef).
			// clusterctl move would error here — predicate stays false.
			name: "one Machine Provisioned without Node (rolling update) → not ready",
			preExist: []runtime.Object{
				runningWithNode("cp-old"),
				provisionedNoNode("cp-new"),
			},
			wantReady:    false,
			wantRowCount: 2,
		},
		{
			name:         "empty Machine list → not ready",
			preExist:     []runtime.Object{},
			wantReady:    false,
			wantRowCount: 0,
		},
		{
			// Belt-and-suspenders: Phase=Running but nodeRef somehow nil.
			// Defensive — CAPI shouldn't produce this state, but if it
			// did the move predicate must still hold to nil-safe behaviour.
			name: "Phase=Running without NodeRef → not ready",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "weird",
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseRunning),
					},
				},
			},
			wantReady:    false,
			wantRowCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() {
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
			})
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...).
				Build()

			rows, ready, err := summarizeMachinesForPivot(context.Background(), fakeClient)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReady, ready)
			require.Len(t, rows, tc.wantRowCount)
		})
	}
}

// TestWaitForAllMachinesRunning is the end-to-end-ish counterpart: it
// drives the wait loop on a fake client and asserts ready / timeout
// behaviour without sitting through the production poll interval.
func TestWaitForAllMachinesRunning(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	tests := []struct {
		name       string
		preExist   []runtime.Object
		ctxTimeout time.Duration
		wantErr    bool
	}{
		{
			name: "returns nil when all Machines are Running with NodeRef",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "cp-1",
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseRunning),
						NodeRef: &coreV1.ObjectReference{
							Kind: "Node",
							Name: "cp-1",
						},
					},
				},
				// The Node backing cp-1 — WaitForAllMachinesRunning's
				// success path renders a `kubectl get nodes`-style table
				// from the main-cluster client, so give it a Node to find.
				&coreV1.Node{
					ObjectMeta: metaV1.ObjectMeta{
						Name:   "cp-1",
						Labels: map[string]string{kubeadmConstants.LabelNodeRoleControlPlane: ""},
					},
					Status: coreV1.NodeStatus{
						Conditions: []coreV1.NodeCondition{
							{Type: coreV1.NodeReady, Status: coreV1.ConditionTrue},
						},
						NodeInfo:  coreV1.NodeSystemInfo{KubeletVersion: "v1.33.0"},
						Addresses: []coreV1.NodeAddress{{Type: coreV1.NodeInternalIP, Address: "10.0.0.5"}},
					},
				},
			},
			ctxTimeout: 5 * time.Second,
		},
		{
			name: "errors when a Machine is stuck Provisioned without NodeRef",
			preExist: []runtime.Object{
				&clusterAPIV1Beta1.Machine{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "cp-stuck",
						Namespace: testCapiClusterNamespace,
					},
					Status: clusterAPIV1Beta1.MachineStatus{
						Phase: string(clusterAPIV1Beta1.MachinePhaseProvisioned),
					},
				},
			},
			ctxTimeout: 500 * time.Millisecond,
			wantErr:    true,
		},
		{
			// Empty Machine list shouldn't short-circuit to ready — the
			// operator would otherwise silently pivot an empty namespace.
			name:       "errors when there are no Machines at all",
			preExist:   []runtime.Object{},
			ctxTimeout: 500 * time.Millisecond,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			origPoll := capiWaitPollInterval
			origTotal := capiWaitTotalTimeout
			t.Cleanup(func() {
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
				capiWaitPollInterval = origPoll
				capiWaitTotalTimeout = origTotal
			})
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil
			// Same shaved poll cadence as TestWaitForMainClusterToBeProvisioned
			// — exercises both the first-tick-success path and the
			// timeout path in sub-second wall time.
			capiWaitPollInterval = 50 * time.Millisecond
			capiWaitTotalTimeout = 2 * time.Second

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...).
				Build()

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			err := WaitForAllMachinesRunning(ctx, fakeClient, fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWaitForMainClusterToBeReady(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	tests := []struct {
		name          string
		nodes         []coreV1.Node
		interceptList func(callCount *atomic.Int32) interceptor.Funcs
		ctxTimeout    time.Duration
		wantErr       bool
	}{
		{
			name: "returns nil when initialized worker node exists",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{Name: "worker-1"},
				},
			},
			ctxTimeout: 5 * time.Second,
		},
		{
			name: "control-plane-only nodes cause context timeout",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name:   "cp-1",
						Labels: map[string]string{kubeadmConstants.LabelNodeRoleControlPlane: ""},
					},
				},
			},
			ctxTimeout: 50 * time.Millisecond,
			wantErr:    true,
		},
		{
			name: "worker with cloud-provider uninitialized taint causes context timeout",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{Name: "worker-tainted"},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{
							{
								Key:    "node.cloudprovider.kubernetes.io/uninitialized",
								Effect: coreV1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			ctxTimeout: 50 * time.Millisecond,
			wantErr:    true,
		},
		{
			name: "worker with cluster-api uninitialized taint causes context timeout",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{Name: "worker-capi-tainted"},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{
							{
								Key:    "node.cluster.x-k8s.io/uninitialized",
								Effect: coreV1.TaintEffectNoSchedule,
							},
						},
					},
				},
			},
			ctxTimeout: 50 * time.Millisecond,
			wantErr:    true,
		},
		{
			name: "list error then success returns nil",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{Name: "worker-ok"},
				},
			},
			interceptList: func(callCount *atomic.Int32) interceptor.Funcs {
				return interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if callCount.Add(1) == 1 {
							return fmt.Errorf("transient network error")
						}
						return c.List(ctx, list, opts...)
					},
				}
			},
			ctxTimeout: 5 * time.Second,
		},
		{
			name:  "context cancelled while list always fails",
			nodes: nil,
			interceptList: func(_ *atomic.Int32) interceptor.Funcs {
				return interceptor.Funcs{
					List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
						return fmt.Errorf("permanent failure")
					},
				}
			},
			ctxTimeout: 50 * time.Millisecond,
			wantErr:    true,
		},
	}

	// Mutates waitForProvisioningPollInterval — sequential only.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origInterval := waitForProvisioningPollInterval
			t.Cleanup(func() { waitForProvisioningPollInterval = origInterval })
			waitForProvisioningPollInterval = time.Millisecond

			var runtimeObjs []client.Object
			for i := range tc.nodes {
				runtimeObjs = append(runtimeObjs, &tc.nodes[i])
			}

			builder := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(runtimeObjs...)

			var callCount atomic.Int32
			if tc.interceptList != nil {
				builder = builder.WithInterceptorFuncs(tc.interceptList(&callCount))
			}

			fakeClient := builder.Build()

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			err := WaitForMainClusterToBeReady(ctx, fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSaveProvisionedClusterKubeconfig(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	kubeconfigData := []byte("apiVersion: v1\nkind: Config\n")

	tests := []struct {
		name         string
		secret       *coreV1.Secret
		interceptGet func(callCount *atomic.Int32, realClient client.Client) interceptor.Funcs
		outputPath   string
		ctxTimeout   time.Duration
		wantErr      bool
		wantContent  []byte
	}{
		{
			name: "writes kubeconfig when secret exists",
			secret: &coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      fmt.Sprintf("%s-kubeconfig", testClusterName),
					Namespace: testCapiClusterNamespace,
				},
				Data: map[string][]byte{"value": kubeconfigData},
			},
			ctxTimeout:  5 * time.Second,
			wantContent: kubeconfigData,
		},
		{
			name: "retries when secret not found initially then succeeds",
			secret: &coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      fmt.Sprintf("%s-kubeconfig", testClusterName),
					Namespace: testCapiClusterNamespace,
				},
				Data: map[string][]byte{"value": kubeconfigData},
			},
			interceptGet: func(callCount *atomic.Int32, realClient client.Client) interceptor.Funcs {
				return interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if callCount.Add(1) == 1 {
							return fmt.Errorf("not found yet")
						}
						return realClient.Get(ctx, key, obj, opts...)
					},
				}
			},
			ctxTimeout:  5 * time.Second,
			wantContent: kubeconfigData,
		},
		{
			name:   "returns context error when secret never exists",
			secret: nil,
			interceptGet: func(_ *atomic.Int32, _ client.Client) interceptor.Funcs {
				return interceptor.Funcs{
					Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
						return fmt.Errorf("not found")
					},
				}
			},
			ctxTimeout: 50 * time.Millisecond,
			wantErr:    true,
		},
		{
			name: "returns error when output path is invalid",
			secret: &coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      fmt.Sprintf("%s-kubeconfig", testClusterName),
					Namespace: testCapiClusterNamespace,
				},
				Data: map[string][]byte{"value": kubeconfigData},
			},
			outputPath: "/nonexistent/dir/kubeconfig",
			ctxTimeout: 5 * time.Second,
			wantErr:    true,
		},
	}

	// Mutates saveKubeconfigPollInterval, outputPathMainClusterKubeconfig,
	// and config.ParsedGeneralConfig — sequential only.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origInterval := saveKubeconfigPollInterval
			origOutputPath := outputPathMainClusterKubeconfig
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() {
				saveKubeconfigPollInterval = origInterval
				outputPathMainClusterKubeconfig = origOutputPath
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
			})

			saveKubeconfigPollInterval = time.Millisecond
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil

			outPath := tc.outputPath
			if outPath == "" {
				outPath = filepath.Join(t.TempDir(), "kubeconfig.yaml")
			}
			outputPathMainClusterKubeconfig = outPath

			var objs []client.Object
			if tc.secret != nil {
				objs = append(objs, tc.secret)
			}

			baseClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			var finalClient client.Client
			if tc.interceptGet != nil {
				var callCount atomic.Int32
				finalClient = crFake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(objs...).
					WithInterceptorFuncs(tc.interceptGet(&callCount, baseClient)).
					Build()
			} else {
				finalClient = baseClient
			}

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			err := SaveProvisionedClusterKubeconfig(ctx, finalClient)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tc.wantContent != nil {
				got, readErr := os.ReadFile(outPath)
				require.NoError(t, readErr)
				assert.Equal(t, tc.wantContent, got)
			}
		})
	}
}

func TestIsClusterctlMoveExecuted(t *testing.T) {
	scheme := newClusterAPITestScheme(t)

	tests := []struct {
		name           string
		createClientFn func(ctx context.Context, kubeconfigPath string) (client.Client, error)
		want           bool
	}{
		{
			name: "returns false when client creation fails",
			createClientFn: func(_ context.Context, _ string) (client.Client, error) {
				return nil, fmt.Errorf("kubeconfig not found")
			},
			want: false,
		},
		{
			name: "returns false when cluster resource does not exist",
			createClientFn: func(_ context.Context, _ string) (client.Client, error) {
				return crFake.NewClientBuilder().
					WithScheme(scheme).
					Build(), nil
			},
			want: false,
		},
		{
			name: "returns true when cluster resource exists",
			createClientFn: func(_ context.Context, _ string) (client.Client, error) {
				return crFake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&clusterAPIV1Beta1.Cluster{
						ObjectMeta: metaV1.ObjectMeta{
							Name:      testClusterName,
							Namespace: testCapiClusterNamespace,
						},
					}).
					Build(), nil
			},
			want: true,
		},
	}

	// Mutates createKubernetesClientFn, outputPathMainClusterKubeconfig,
	// and config.ParsedGeneralConfig — sequential only.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origFn := createKubernetesClientFn
			origPath := outputPathMainClusterKubeconfig
			origName := config.ParsedGeneralConfig.Cluster.Name
			origObmondo := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() {
				createKubernetesClientFn = origFn
				outputPathMainClusterKubeconfig = origPath
				config.ParsedGeneralConfig.Cluster.Name = origName
				config.ParsedGeneralConfig.Obmondo = origObmondo
			})

			createKubernetesClientFn = tc.createClientFn
			outputPathMainClusterKubeconfig = filepath.Join(t.TempDir(), "kubeconfig.yaml")
			config.ParsedGeneralConfig.Cluster.Name = testClusterName
			config.ParsedGeneralConfig.Obmondo = nil

			got := IsClusterctlMoveExecuted(context.Background())
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestSummarizeCPNodesNetworking covers the predicate WaitForCPNodesNetworkingReady
// loops on: control-plane Nodes must report Ready=True AND
// NetworkUnavailable=False. We assert the helper directly so the table
// stays readable; the surrounding wait loop is exercised in
// TestWaitForCPNodesNetworkingReady below.
func TestSummarizeCPNodesNetworking(t *testing.T) {
	cpLabels := map[string]string{kubeadmConstants.LabelNodeRoleControlPlane: ""}

	makeNode := func(name string, labels map[string]string, ready, netUnavailable *coreV1.ConditionStatus) coreV1.Node {
		conds := []coreV1.NodeCondition{}
		if ready != nil {
			conds = append(conds, coreV1.NodeCondition{Type: coreV1.NodeReady, Status: *ready})
		}
		if netUnavailable != nil {
			conds = append(conds, coreV1.NodeCondition{Type: coreV1.NodeNetworkUnavailable, Status: *netUnavailable})
		}
		return coreV1.Node{
			ObjectMeta: metaV1.ObjectMeta{Name: name, Labels: labels},
			Status:     coreV1.NodeStatus{Conditions: conds},
		}
	}

	condTrue := coreV1.ConditionTrue
	condFalse := coreV1.ConditionFalse

	tests := []struct {
		name                string
		nodes               []coreV1.Node
		wantReady           bool
		wantReasonsContains []string
	}{
		{
			name: "single CP Ready=True, NetworkUnavailable=False → ready",
			nodes: []coreV1.Node{
				makeNode("cp-1", cpLabels, &condTrue, &condFalse),
			},
			wantReady: true,
		},
		{
			name: "single CP Ready=True, NetworkUnavailable absent → ready (absent = available)",
			nodes: []coreV1.Node{
				makeNode("cp-1", cpLabels, &condTrue, nil),
			},
			wantReady: true,
		},
		{
			// Real-world case the user hit: cilium install rolled back,
			// kubelet reports NodeReady but networking unavailable.
			name: "CP Ready=True, NetworkUnavailable=True → not ready",
			nodes: []coreV1.Node{
				makeNode("cp-1", cpLabels, &condTrue, &condTrue),
			},
			wantReady:           false,
			wantReasonsContains: []string{"cp-1: NetworkUnavailable=True"},
		},
		{
			name: "CP Ready=False → not ready",
			nodes: []coreV1.Node{
				makeNode("cp-1", cpLabels, &condFalse, &condFalse),
			},
			wantReady:           false,
			wantReasonsContains: []string{"cp-1: Ready!=True"},
		},
		{
			// Multi-CP rolling update: one CP healthy, the surge
			// replacement still has NetworkUnavailable=True.
			name: "one CP healthy, one mid-bootstrap → not ready",
			nodes: []coreV1.Node{
				makeNode("cp-old", cpLabels, &condTrue, &condFalse),
				makeNode("cp-new", cpLabels, &condTrue, &condTrue),
			},
			wantReady:           false,
			wantReasonsContains: []string{"cp-new: NetworkUnavailable=True"},
		},
		{
			// Workers shouldn't influence the predicate at all — we only
			// gate on control-plane Nodes. Unlabelled / non-CP-labelled
			// Nodes are ignored.
			name: "worker NetworkUnavailable=True, CP healthy → ready (workers ignored)",
			nodes: []coreV1.Node{
				makeNode("cp-1", cpLabels, &condTrue, &condFalse),
				makeNode("worker-1", nil, &condTrue, &condTrue),
			},
			wantReady: true,
		},
		{
			// Defensive: we shouldn't reach this wait with no CP Nodes
			// registered (CAPI's Cluster Ready gate requires the control
			// plane to be initialized), so the empty case is a misuse,
			// not a success.
			name:                "empty Node list → not ready",
			nodes:               []coreV1.Node{},
			wantReady:           false,
			wantReasonsContains: []string{"no control-plane Nodes found"},
		},
		{
			name: "only workers, no CP → not ready",
			nodes: []coreV1.Node{
				makeNode("worker-1", nil, &condTrue, &condFalse),
			},
			wantReady:           false,
			wantReasonsContains: []string{"no control-plane Nodes found"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reasons := summarizeCPNodesNetworking(&coreV1.NodeList{Items: tc.nodes})
			assert.Equal(t, tc.wantReady, got)
			for _, want := range tc.wantReasonsContains {
				found := false
				for _, r := range reasons {
					if containsSubstr(r, want) {
						found = true
						break
					}
				}
				assert.True(t, found, "expected reason containing %q; got %v", want, reasons)
			}
		})
	}
}

// TestWaitForCPNodesNetworkingReady is the end-to-end-ish counterpart:
// it drives the wait loop on a fake client and asserts ready / timeout
// behaviour without sitting through the production poll interval.
func TestWaitForCPNodesNetworkingReady(t *testing.T) {
	scheme := newClusterAPITestScheme(t)
	cpLabels := map[string]string{kubeadmConstants.LabelNodeRoleControlPlane: ""}

	tests := []struct {
		name       string
		preExist   []runtime.Object
		ctxTimeout time.Duration
		wantErr    bool
	}{
		{
			name: "returns nil when CP Node is Ready=True and NetworkUnavailable=False",
			preExist: []runtime.Object{
				&coreV1.Node{
					ObjectMeta: metaV1.ObjectMeta{Name: "cp-1", Labels: cpLabels},
					Status: coreV1.NodeStatus{Conditions: []coreV1.NodeCondition{
						{Type: coreV1.NodeReady, Status: coreV1.ConditionTrue},
						{Type: coreV1.NodeNetworkUnavailable, Status: coreV1.ConditionFalse},
					}},
				},
			},
			ctxTimeout: 5 * time.Second,
		},
		{
			name: "errors when CP Node has NetworkUnavailable=True (cilium-rollback case)",
			preExist: []runtime.Object{
				&coreV1.Node{
					ObjectMeta: metaV1.ObjectMeta{Name: "cp-1", Labels: cpLabels},
					Status: coreV1.NodeStatus{Conditions: []coreV1.NodeCondition{
						{Type: coreV1.NodeReady, Status: coreV1.ConditionTrue},
						{Type: coreV1.NodeNetworkUnavailable, Status: coreV1.ConditionTrue},
					}},
				},
			},
			ctxTimeout: 500 * time.Millisecond,
			wantErr:    true,
		},
		{
			name:       "errors when no CP Nodes are registered",
			preExist:   []runtime.Object{},
			ctxTimeout: 500 * time.Millisecond,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origPoll := waitForProvisioningPollInterval
			origTotal := waitForCPNodesNetworkingTimeout
			t.Cleanup(func() {
				waitForProvisioningPollInterval = origPoll
				waitForCPNodesNetworkingTimeout = origTotal
			})
			// Sub-second polling + tight total so the timeout path
			// runs in well under a second of wall clock.
			waitForProvisioningPollInterval = 50 * time.Millisecond
			waitForCPNodesNetworkingTimeout = 2 * time.Second

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...).
				Build()

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			err := WaitForCPNodesNetworkingReady(ctx, fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
