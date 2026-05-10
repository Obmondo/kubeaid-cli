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
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeadmConstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

const (
	testClusterName          = "test-cluster"
	testCapiClusterNamespace = "capi-cluster"
)

// newClusterAPITestScheme builds a scheme that includes coreV1 and
// cluster-api types. summarizeCAPIStatus is intentionally cloud-agnostic
// — it reads only Cluster + Machine, never provider-specific types —
// so we don't register CAPH/CAPA/CAPZ scheme here.
func newClusterAPITestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	require.NoError(t, clusterAPIV1Beta1.AddToScheme(s))
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
	tests := []struct {
		name    string
		obmondo *config.ObmondoConfig
		want    string
	}{
		{
			name:    "no Obmondo config returns default namespace",
			obmondo: nil,
			want:    testCapiClusterNamespace,
		},
		{
			name:    "Obmondo config with empty CustomerID returns default namespace",
			obmondo: &config.ObmondoConfig{CustomerID: ""},
			want:    testCapiClusterNamespace,
		},
		{
			name:    "Obmondo config with CustomerID returns namespaced variant",
			obmondo: &config.ObmondoConfig{CustomerID: "acme"},
			want:    "capi-cluster-acme",
		},
		{
			name:    "CustomerID with hyphens is preserved",
			obmondo: &config.ObmondoConfig{CustomerID: "my-company-123"},
			want:    "capi-cluster-my-company-123",
		},
	}

	// Mutates ParsedGeneralConfig — sequential only.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := config.ParsedGeneralConfig.Obmondo
			t.Cleanup(func() { config.ParsedGeneralConfig.Obmondo = original })
			config.ParsedGeneralConfig.Obmondo = tc.obmondo

			got := GetCapiClusterNamespace()
			assert.Equal(t, tc.want, got)
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
