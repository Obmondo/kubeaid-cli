// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	kubeadmConstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	require.NoError(t, appsV1.AddToScheme(s))
	require.NoError(t, batchV1.AddToScheme(s))
	return s
}

func TestGetManagementClusterKubeconfigPath(t *testing.T) {
	t.Parallel()

	got, err := GetManagementClusterKubeconfigPath(context.Background())
	require.NoError(t, err)
	assert.Equal(t, constants.OutputPathManagementClusterHostKubeconfig, got)
}

func TestCreateKubernetesClient(t *testing.T) {
	tests := []struct {
		name             string
		kubeconfigPath   string
		overrideClientFn func(*rest.Config, client.Options) (client.Client, error)
		overridePingFn   func(context.Context, client.Client) error
		wantErrSubstr    string
	}{
		{
			name:           "nonexistent kubeconfig file — returns error",
			kubeconfigPath: filepath.Join(t.TempDir(), "nonexistent-kubeconfig"),
			wantErrSubstr:  "failed building config",
		},
		{
			name: "invalid kubeconfig content — returns error",
			kubeconfigPath: func() string {
				f, err := os.CreateTemp(t.TempDir(), "bad-kubeconfig")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.Write([]byte("not-a-kubeconfig"))
				f.Close()
				return f.Name()
			}(),
			wantErrSubstr: "failed building config",
		},
		{
			name: "empty YAML kubeconfig — returns error",
			kubeconfigPath: func() string {
				f, err := os.CreateTemp(t.TempDir(), "empty-kubeconfig")
				if err != nil {
					t.Fatal(err)
				}
				f.Close()
				return f.Name()
			}(),
			wantErrSubstr: "failed building config",
		},
		{
			name:           "returns error when client.New fails",
			kubeconfigPath: "../../../testdata/kubernetes/valid_kubeconfig.yaml",
			overrideClientFn: func(_ *rest.Config, _ client.Options) (client.Client, error) {
				return nil, errors.New("dial tcp: connection refused")
			},
			wantErrSubstr: "failed creating kube client",
		},
		{
			name:           "returns error when ping fails",
			kubeconfigPath: "../../../testdata/kubernetes/valid_kubeconfig.yaml",
			overrideClientFn: func(_ *rest.Config, _ client.Options) (client.Client, error) {
				return crFake.NewClientBuilder().WithScheme(newTestScheme(t)).Build(), nil
			},
			overridePingFn: func(_ context.Context, _ client.Client) error {
				return errors.New("cluster unreachable")
			},
			wantErrSubstr: "cluster unreachable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origClientFn := newClientFn
			origPingFn := pingKubernetesClusterFn
			t.Cleanup(func() {
				newClientFn = origClientFn
				pingKubernetesClusterFn = origPingFn
			})

			if tc.overrideClientFn != nil {
				newClientFn = tc.overrideClientFn
			}
			if tc.overridePingFn != nil {
				pingKubernetesClusterFn = tc.overridePingFn
			}

			_, err := CreateKubernetesClient(context.Background(), tc.kubeconfigPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrSubstr)
		})
	}
}

func TestPingKubernetesCluster(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme(t)

	tests := []struct {
		name          string
		buildClient   func() client.Client
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "succeeds when cluster is reachable and deployment list is empty",
			buildClient: func() client.Client {
				return crFake.NewClientBuilder().WithScheme(scheme).Build()
			},
		},
		{
			name: "succeeds when cluster has deployments in default namespace",
			buildClient: func() client.Client {
				return crFake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
					&appsV1.Deployment{
						ObjectMeta: metaV1.ObjectMeta{
							Name:      "my-deployment",
							Namespace: "default",
						},
					},
				).Build()
			},
		},
		{
			name: "returns error when listing deployments fails",
			buildClient: func() client.Client {
				return crFake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
					List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
						return fmt.Errorf("connection refused")
					},
				}).Build()
			},
			wantErr:       true,
			wantErrSubstr: "failed pinging the kubernetes cluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := pingKubernetesCluster(context.Background(), tc.buildClient())

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGetMainClusterEndpoint(t *testing.T) {
	const testClusterName = "test-cluster"
	const testServerURL = "https://10.0.0.1:6443"

	tests := []struct {
		name                   string
		loadKubeConfig         func(string) (*clientcmdapi.Config, error)
		createKubernetesClient func(context.Context, string) (client.Client, error)
		pingKubernetesCluster  func(context.Context, client.Client) error
		clusterName            string
		want                   *url.URL
		wantErr                bool
		wantErrSubstr          string
	}{
		{
			name: "kubeconfig file does not exist returns nil",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return nil, os.ErrNotExist
			},
			want: nil,
		},
		{
			name: "loadKubeConfigFromFile returns non-ErrNotExist error",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return nil, fmt.Errorf("corrupted kubeconfig")
			},
			wantErr:       true,
			wantErrSubstr: "failed reading main cluster's kubeconfig file",
		},
		{
			name: "kubeconfig exists but cluster name not found returns nil",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{
					Clusters: map[string]*clientcmdapi.Cluster{
						"other-cluster": {Server: testServerURL},
					},
				}, nil
			},
			clusterName: testClusterName,
			want:        nil,
		},
		{
			name: "url.Parse fails on invalid server URL",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{
					Clusters: map[string]*clientcmdapi.Cluster{
						testClusterName: {Server: "://bad"},
					},
				}, nil
			},
			clusterName:   testClusterName,
			wantErr:       true,
			wantErrSubstr: "failed parsing main cluster's API server endpoint",
		},
		{
			name: "cluster found but CreateKubernetesClient fails returns nil",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{
					Clusters: map[string]*clientcmdapi.Cluster{
						testClusterName: {Server: testServerURL},
					},
				}, nil
			},
			createKubernetesClient: func(_ context.Context, _ string) (client.Client, error) {
				return nil, errors.New("connection refused")
			},
			clusterName: testClusterName,
			want:        nil,
		},
		{
			name: "cluster found and client created but ping fails returns nil",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{
					Clusters: map[string]*clientcmdapi.Cluster{
						testClusterName: {Server: testServerURL},
					},
				}, nil
			},
			createKubernetesClient: func(_ context.Context, _ string) (client.Client, error) {
				return crFake.NewClientBuilder().WithScheme(newTestScheme(t)).Build(), nil
			},
			pingKubernetesCluster: func(_ context.Context, _ client.Client) error {
				return errors.New("cluster unreachable")
			},
			clusterName: testClusterName,
			want:        nil,
		},
		{
			name: "happy path returns parsed endpoint URL",
			loadKubeConfig: func(_ string) (*clientcmdapi.Config, error) {
				return &clientcmdapi.Config{
					Clusters: map[string]*clientcmdapi.Cluster{
						testClusterName: {Server: testServerURL},
					},
				}, nil
			},
			createKubernetesClient: func(_ context.Context, _ string) (client.Client, error) {
				return crFake.NewClientBuilder().WithScheme(newTestScheme(t)).Build(), nil
			},
			pingKubernetesCluster: func(_ context.Context, _ client.Client) error {
				return nil
			},
			clusterName: testClusterName,
			want: func() *url.URL {
				u, _ := url.Parse(testServerURL)
				return u
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origLoadFn := loadKubeConfigFromFileFn
			origCreateFn := createKubernetesClientFn
			origPingFn := pingKubernetesClusterFn
			origConfig := *config.ParsedGeneralConfig
			t.Cleanup(func() {
				loadKubeConfigFromFileFn = origLoadFn
				createKubernetesClientFn = origCreateFn
				pingKubernetesClusterFn = origPingFn
				*config.ParsedGeneralConfig = origConfig
			})

			loadKubeConfigFromFileFn = tc.loadKubeConfig

			if tc.createKubernetesClient != nil {
				createKubernetesClientFn = tc.createKubernetesClient
			}
			if tc.pingKubernetesCluster != nil {
				pingKubernetesClusterFn = tc.pingKubernetesCluster
			}

			if tc.clusterName != "" {
				config.ParsedGeneralConfig.Cluster.Name = tc.clusterName
			}

			got, err := GetMainClusterEndpoint(context.Background())
			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tc.wantErrSubstr)
				}
				return
			}
			require.NoError(t, err)

			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want.String(), got.String())
		})
	}
}

func TestCreateNamespace(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme(t)

	tests := []struct {
		name            string
		namespaceName   string
		preExist        []runtime.Object
		interceptCreate func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error
		wantErr         bool
		wantErrSubstr   string
	}{
		{
			name:          "creates namespace when it does not exist",
			namespaceName: "new-namespace",
			preExist:      []runtime.Object{},
		},
		{
			name:          "is idempotent when namespace already exists",
			namespaceName: "already-exists",
			preExist: []runtime.Object{
				&coreV1.Namespace{
					ObjectMeta: metaV1.ObjectMeta{Name: "already-exists"},
				},
			},
		},
		{
			name:          "returns error when Create fails with non-AlreadyExists error",
			namespaceName: "fail-ns",
			interceptCreate: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
				return errors.New("internal server error")
			},
			wantErr:       true,
			wantErrSubstr: "failed creating namespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			builder := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...)

			if tc.interceptCreate != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Create: tc.interceptCreate,
				})
			}

			fakeClient := builder.Build()

			err := CreateNamespace(context.Background(), tc.namespaceName, fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tc.wantErrSubstr)
				}
				return
			}
			require.NoError(t, err)

			ns := &coreV1.Namespace{}
			err = fakeClient.Get(context.Background(),
				types.NamespacedName{Name: tc.namespaceName},
				ns,
			)
			require.NoError(t, err)
			assert.Equal(t, tc.namespaceName, ns.Name)
		})
	}
}

func TestGetKubernetesResource(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme(t)

	tests := []struct {
		name     string
		preExist []runtime.Object
		lookup   client.Object
		wantErr  bool
		wantName string
	}{
		{
			name: "returns cluster-scoped resource when it exists",
			preExist: []runtime.Object{
				&coreV1.Namespace{ObjectMeta: metaV1.ObjectMeta{Name: "existing-ns"}},
			},
			lookup:   &coreV1.Namespace{ObjectMeta: metaV1.ObjectMeta{Name: "existing-ns"}},
			wantName: "existing-ns",
		},
		{
			name:     "returns not-found error when cluster-scoped resource does not exist",
			preExist: []runtime.Object{},
			lookup:   &coreV1.Namespace{ObjectMeta: metaV1.ObjectMeta{Name: "missing-ns"}},
			wantErr:  true,
		},
		{
			name: "returns namespaced ConfigMap when it exists",
			preExist: []runtime.Object{
				&coreV1.ConfigMap{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "my-config",
						Namespace: "kube-system",
					},
					Data: map[string]string{"key": "value"},
				},
			},
			lookup: &coreV1.ConfigMap{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "my-config",
					Namespace: "kube-system",
				},
			},
			wantName: "my-config",
		},
		{
			name: "returns not-found when ConfigMap exists in different namespace",
			preExist: []runtime.Object{
				&coreV1.ConfigMap{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      "my-config",
						Namespace: "kube-system",
					},
				},
			},
			lookup: &coreV1.ConfigMap{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "my-config",
					Namespace: "default",
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tc.preExist...).
				Build()

			err := GetKubernetesResource(context.Background(), fakeClient, tc.lookup)

			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, k8sAPIErrors.IsNotFound(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, tc.lookup.GetName())
		})
	}
}

func TestIsControlPlaneNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node *coreV1.Node
		want bool
	}{
		{
			name: "node with control-plane label returns true",
			node: &coreV1.Node{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						kubeadmConstants.LabelNodeRoleControlPlane: "",
					},
				},
			},
			want: true,
		},
		{
			name: "node without control-plane label returns false",
			node: &coreV1.Node{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						"kubernetes.io/hostname": "worker-1",
					},
				},
			},
			want: false,
		},
		{
			name: "node with no labels returns false",
			node: &coreV1.Node{
				ObjectMeta: metaV1.ObjectMeta{},
			},
			want: false,
		},
		{
			name: "node with control-plane label alongside other labels returns true",
			node: &coreV1.Node{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						"beta.kubernetes.io/arch":                  "amd64",
						kubeadmConstants.LabelNodeRoleControlPlane: "",
						"kubernetes.io/hostname":                   "master-0",
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isControlPlaneNode(tc.node)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTriggerCRONJob(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme(t)

	tests := []struct {
		name            string
		cronJobName     string
		namespace       string
		image           string
		preExist        []runtime.Object
		interceptCreate func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error
		wantErr         bool
		wantErrSubstr   string
	}{
		{
			name:        "creates Job from CronJob template in default namespace",
			cronJobName: "my-cronjob",
			namespace:   "default",
			image:       "busybox",
		},
		{
			name:        "creates Job from CronJob template in custom namespace",
			cronJobName: "backup-cronjob",
			namespace:   "velero",
			image:       "velero/velero:latest",
		},
		{
			name:          "returns error when CronJob does not exist",
			cronJobName:   "missing-cronjob",
			namespace:     "default",
			preExist:      []runtime.Object{},
			wantErr:       true,
			wantErrSubstr: "failed getting CRONJob",
		},
		{
			name:        "returns error when Job creation fails",
			cronJobName: "fail-create-cronjob",
			namespace:   "default",
			image:       "busybox",
			interceptCreate: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
				if _, ok := obj.(*batchV1.Job); ok {
					return errors.New("quota exceeded")
				}
				return nil
			},
			wantErr:       true,
			wantErrSubstr: "failed creating Job from CRONJob",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var runtimeObjs []runtime.Object
			if tc.preExist != nil {
				runtimeObjs = tc.preExist
			} else {
				cronJob := &batchV1.CronJob{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      tc.cronJobName,
						Namespace: tc.namespace,
					},
					Spec: batchV1.CronJobSpec{
						JobTemplate: batchV1.JobTemplateSpec{
							Spec: batchV1.JobSpec{
								Template: coreV1.PodTemplateSpec{
									Spec: coreV1.PodSpec{
										Containers: []coreV1.Container{
											{
												Name:    "worker",
												Image:   tc.image,
												Command: []string{"echo", "hello"},
											},
										},
										RestartPolicy: coreV1.RestartPolicyNever,
									},
								},
							},
						},
					},
				}
				runtimeObjs = []runtime.Object{cronJob}
			}

			builder := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(runtimeObjs...)

			if tc.interceptCreate != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Create: tc.interceptCreate,
				})
			}

			fakeClient := builder.Build()

			err := TriggerCRONJob(context.Background(),
				client.ObjectKey{Name: tc.cronJobName, Namespace: tc.namespace},
				fakeClient,
			)
			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tc.wantErrSubstr)
				}
				return
			}
			require.NoError(t, err)

			jobList := &batchV1.JobList{}
			require.NoError(t, fakeClient.List(context.Background(), jobList,
				client.InNamespace(tc.namespace),
			))

			require.GreaterOrEqual(t, len(jobList.Items), 1, "at least one Job should be created")
			createdJob := jobList.Items[0]
			assert.Equal(t, tc.namespace, createdJob.Namespace)
			require.Len(t, createdJob.Spec.Template.Spec.Containers, 1)
			assert.Equal(t, tc.image, createdJob.Spec.Template.Spec.Containers[0].Image)
		})
	}
}

func TestIsNodeGroupCountZero(t *testing.T) {
	tests := []struct {
		name          string
		cloudProvider string
		setupConfig   func()
		want          bool
	}{
		{
			name:          "AWS with no node groups returns true",
			cloudProvider: constants.CloudProviderAWS,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.AWS = &config.AWSConfig{
					NodeGroups: nil,
				}
			},
			want: true,
		},
		{
			name:          "AWS with node groups returns false",
			cloudProvider: constants.CloudProviderAWS,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.AWS = &config.AWSConfig{
					NodeGroups: []config.AWSAutoScalableNodeGroup{{}},
				}
			},
			want: false,
		},
		{
			name:          "Azure with no node groups returns true",
			cloudProvider: constants.CloudProviderAzure,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.Azure = &config.AzureConfig{
					NodeGroups: nil,
				}
			},
			want: true,
		},
		{
			name:          "Azure with node groups returns false",
			cloudProvider: constants.CloudProviderAzure,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.Azure = &config.AzureConfig{
					NodeGroups: []config.AzureAutoScalableNodeGroup{{}},
				}
			},
			want: false,
		},
		{
			name:          "BareMetal with no node groups returns true",
			cloudProvider: constants.CloudProviderBareMetal,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.BareMetal = &config.BareMetalConfig{
					NodeGroups: nil,
				}
			},
			want: true,
		},
		{
			name:          "BareMetal with node groups returns false",
			cloudProvider: constants.CloudProviderBareMetal,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.BareMetal = &config.BareMetalConfig{
					NodeGroups: []config.BareMetalNodeGroup{{}},
				}
			},
			want: false,
		},
		{
			name:          "Hetzner with no HCloud and no BareMetal node groups returns true",
			cloudProvider: constants.CloudProviderHetzner,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.Hetzner = &config.HetznerConfig{
					NodeGroups: config.HetznerNodeGroups{
						HCloud:    nil,
						BareMetal: nil,
					},
				}
			},
			want: true,
		},
		{
			name:          "Hetzner with HCloud node groups returns false",
			cloudProvider: constants.CloudProviderHetzner,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.Hetzner = &config.HetznerConfig{
					NodeGroups: config.HetznerNodeGroups{
						HCloud:    []config.HCloudAutoScalableNodeGroup{{}},
						BareMetal: nil,
					},
				}
			},
			want: false,
		},
		{
			name:          "Hetzner with BareMetal node groups returns false",
			cloudProvider: constants.CloudProviderHetzner,
			setupConfig: func() {
				config.ParsedGeneralConfig.Cloud.Hetzner = &config.HetznerConfig{
					NodeGroups: config.HetznerNodeGroups{
						HCloud:    nil,
						BareMetal: []*config.HetznerBareMetalNodeGroup{{}},
					},
				}
			},
			want: false,
		},
		{
			name:          "unknown cloud provider returns false",
			cloudProvider: "unknown-cloud",
			setupConfig:   func() {},
			want:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalProvider := globals.CloudProviderName
			originalConfig := *config.ParsedGeneralConfig
			t.Cleanup(func() {
				globals.CloudProviderName = originalProvider
				*config.ParsedGeneralConfig = originalConfig
			})

			globals.CloudProviderName = tc.cloudProvider
			tc.setupConfig()

			got := IsNodeGroupCountZero(context.Background())
			assert.Equal(t, tc.want, got)
		})
	}
}

func nodeRuntimeObjects(nodes []coreV1.Node) []runtime.Object {
	objs := make([]coreV1.Node, len(nodes))
	copy(objs, nodes)

	runtimeObjs := make([]runtime.Object, len(objs))
	for i := range objs {
		runtimeObjs[i] = &objs[i]
	}

	return runtimeObjs
}

func countRemovedControlPlaneTaints(t *testing.T, cl client.Client, nodes []coreV1.Node) int {
	t.Helper()

	removedCount := 0
	for _, origNode := range nodes {
		if _, ok := origNode.Labels[kubeadmConstants.LabelNodeRoleControlPlane]; !ok {
			continue
		}

		updatedNode := &coreV1.Node{}
		err := cl.Get(context.Background(), types.NamespacedName{Name: origNode.Name}, updatedNode)
		require.NoError(t, err)

		if hasControlPlaneTaint(origNode.Spec.Taints) && !hasControlPlaneTaint(updatedNode.Spec.Taints) {
			removedCount++
		}
	}

	return removedCount
}

func hasControlPlaneTaint(taints []coreV1.Taint) bool {
	for _, taint := range taints {
		if taint.Key == kubeadmConstants.LabelNodeRoleControlPlane {
			return true
		}
	}

	return false
}

func TestRemoveNoScheduleTaintsFromMasterNodes(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme(t)

	controlPlaneTaint := coreV1.Taint{
		Key:    kubeadmConstants.LabelNodeRoleControlPlane,
		Effect: coreV1.TaintEffectNoSchedule,
	}

	tests := []struct {
		name          string
		nodes         []coreV1.Node
		interceptList func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error
		wantErr       bool
		wantErrSubstr string
		// wantTaintsRemoved is the number of nodes expected to have their
		// control-plane taint removed.
		wantTaintsRemoved int
	}{
		{
			name: "removes taint from single control-plane node",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name: "master-0",
						Labels: map[string]string{
							kubeadmConstants.LabelNodeRoleControlPlane: "",
						},
					},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{controlPlaneTaint},
					},
				},
			},
			wantTaintsRemoved: 1,
		},
		{
			name: "removes taint from multiple control-plane nodes",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name: "master-0",
						Labels: map[string]string{
							kubeadmConstants.LabelNodeRoleControlPlane: "",
						},
					},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{controlPlaneTaint},
					},
				},
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name: "master-1",
						Labels: map[string]string{
							kubeadmConstants.LabelNodeRoleControlPlane: "",
						},
					},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{controlPlaneTaint},
					},
				},
			},
			wantTaintsRemoved: 2,
		},
		{
			name: "no-op when no control-plane nodes exist",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name: "worker-0",
						Labels: map[string]string{
							"kubernetes.io/hostname": "worker-0",
						},
					},
				},
			},
			wantTaintsRemoved: 0,
		},
		{
			name: "no-op when control-plane node has no matching taint",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name: "master-0",
						Labels: map[string]string{
							kubeadmConstants.LabelNodeRoleControlPlane: "",
						},
					},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{
							{Key: "some-other-taint", Effect: coreV1.TaintEffectNoSchedule},
						},
					},
				},
			},
			wantTaintsRemoved: 0,
		},
		{
			name:              "no-op when node list is empty",
			nodes:             []coreV1.Node{},
			wantTaintsRemoved: 0,
		},
		{
			name: "only removes matching taint when node has mixed taints",
			nodes: []coreV1.Node{
				{
					ObjectMeta: metaV1.ObjectMeta{
						Name: "master-0",
						Labels: map[string]string{
							kubeadmConstants.LabelNodeRoleControlPlane: "",
						},
					},
					Spec: coreV1.NodeSpec{
						Taints: []coreV1.Taint{
							{Key: "dedicated", Value: "special", Effect: coreV1.TaintEffectNoSchedule},
							controlPlaneTaint,
							{Key: "another-taint", Effect: coreV1.TaintEffectNoExecute},
						},
					},
				},
			},
			wantTaintsRemoved: 1,
		},
		{
			name: "returns error when List fails",
			interceptList: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("api server unavailable")
			},
			wantErr:       true,
			wantErrSubstr: "failed listing master nodes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			builder := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(nodeRuntimeObjects(tc.nodes)...)

			if tc.interceptList != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					List: tc.interceptList,
				})
			}

			fakeClient := builder.Build()

			err := removeNoScheduleTaintsFromMasterNodes(context.Background(), fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tc.wantErrSubstr)
				}
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tc.wantTaintsRemoved, countRemovedControlPlaneTaints(t, fakeClient, tc.nodes))
		})
	}
}
