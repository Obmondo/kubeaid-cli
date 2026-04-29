// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/project"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	repoApiclient "github.com/argoproj/argo-cd/v2/reposerver/apiclient"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

type fakeArgoCDAppClient struct {
	listResponse *argoCDV1Alpha1.ApplicationList
	listErr      error

	syncResponse *argoCDV1Alpha1.Application
	syncErr      error
	syncCalled   int

	getResponses []fakeGetResponse
	getCalled    int
}

type fakeGetResponse struct {
	app *argoCDV1Alpha1.Application
	err error
}

func (f *fakeArgoCDAppClient) List(_ context.Context, _ *application.ApplicationQuery, _ ...grpc.CallOption) (*argoCDV1Alpha1.ApplicationList, error) {
	return f.listResponse, f.listErr
}

func (f *fakeArgoCDAppClient) Sync(_ context.Context, _ *application.ApplicationSyncRequest, _ ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	f.syncCalled++
	return f.syncResponse, f.syncErr
}

func (f *fakeArgoCDAppClient) Get(_ context.Context, _ *application.ApplicationQuery, _ ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	defer func() { f.getCalled++ }()
	idx := f.getCalled
	if idx >= len(f.getResponses) {
		idx = len(f.getResponses) - 1
	}
	r := f.getResponses[idx]
	return r.app, r.err
}

func syncedApp() *argoCDV1Alpha1.Application {
	return &argoCDV1Alpha1.Application{
		Status: argoCDV1Alpha1.ApplicationStatus{
			Sync: argoCDV1Alpha1.SyncStatus{Status: argoCDV1Alpha1.SyncStatusCodeSynced},
		},
	}
}

func appWithOverallStatus(code argoCDV1Alpha1.SyncStatusCode) *argoCDV1Alpha1.Application {
	return &argoCDV1Alpha1.Application{
		Status: argoCDV1Alpha1.ApplicationStatus{
			Sync: argoCDV1Alpha1.SyncStatus{Status: code},
		},
	}
}

func appWithResourceStatuses(resources []argoCDV1Alpha1.ResourceStatus) *argoCDV1Alpha1.Application {
	return &argoCDV1Alpha1.Application{
		Status: argoCDV1Alpha1.ApplicationStatus{Resources: resources},
	}
}

func alwaysSynced() *fakeArgoCDAppClient {
	return &fakeArgoCDAppClient{
		getResponses: []fakeGetResponse{{app: syncedApp(), err: nil}},
	}
}

type fakeProjectServiceClient struct {
	createResp *argoCDV1Alpha1.AppProject
	createErr  error
}

func (f *fakeProjectServiceClient) Create(_ context.Context, _ *project.ProjectCreateRequest, _ ...grpc.CallOption) (*argoCDV1Alpha1.AppProject, error) {
	return f.createResp, f.createErr
}

func (f *fakeProjectServiceClient) CreateToken(context.Context, *project.ProjectTokenCreateRequest, ...grpc.CallOption) (*project.ProjectTokenResponse, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) DeleteToken(context.Context, *project.ProjectTokenDeleteRequest, ...grpc.CallOption) (*project.EmptyResponse, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) List(context.Context, *project.ProjectQuery, ...grpc.CallOption) (*argoCDV1Alpha1.AppProjectList, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) GetDetailedProject(context.Context, *project.ProjectQuery, ...grpc.CallOption) (*project.DetailedProjectsResponse, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) Get(context.Context, *project.ProjectQuery, ...grpc.CallOption) (*argoCDV1Alpha1.AppProject, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) GetGlobalProjects(context.Context, *project.ProjectQuery, ...grpc.CallOption) (*project.GlobalProjectsResponse, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) Update(context.Context, *project.ProjectUpdateRequest, ...grpc.CallOption) (*argoCDV1Alpha1.AppProject, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) Delete(context.Context, *project.ProjectQuery, ...grpc.CallOption) (*project.EmptyResponse, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) ListEvents(context.Context, *project.ProjectQuery, ...grpc.CallOption) (*coreV1.EventList, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) GetSyncWindowsState(context.Context, *project.SyncWindowsQuery, ...grpc.CallOption) (*project.SyncWindowsResponse, error) {
	return nil, nil
}

func (f *fakeProjectServiceClient) ListLinks(context.Context, *project.ListProjectLinksRequest, ...grpc.CallOption) (*application.LinksResponse, error) {
	return nil, nil
}

func TestCreateArgoCDProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		createResp    *argoCDV1Alpha1.AppProject
		createErr     error
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:       "happy path: create succeeds",
			createResp: &argoCDV1Alpha1.AppProject{},
		},
		{
			name:      "already exists is silently skipped",
			createErr: grpcStatus.Error(codes.AlreadyExists, "already exists"),
		},
		{
			name:          "other gRPC error is returned",
			createErr:     grpcStatus.Error(codes.PermissionDenied, "permission denied"),
			wantErr:       true,
			wantErrSubstr: "failed creating kubeaid ArgoCD project",
		},
		{
			name:          "non-gRPC error is returned",
			createErr:     errors.New("network unreachable"),
			wantErr:       true,
			wantErrSubstr: "failed creating kubeaid ArgoCD project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeProjectServiceClient{
				createResp: tc.createResp,
				createErr:  tc.createErr,
			}

			err := CreateArgoCDProject(context.Background(), fake, "kubeaid")
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

type fakeFullApplicationServiceClient struct {
	fakeArgoCDAppClient
}

func (f *fakeFullApplicationServiceClient) ListResourceEvents(context.Context, *application.ApplicationResourceEventsQuery, ...grpc.CallOption) (*coreV1.EventList, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) Watch(context.Context, *application.ApplicationQuery, ...grpc.CallOption) (application.ApplicationService_WatchClient, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) Create(context.Context, *application.ApplicationCreateRequest, ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) GetApplicationSyncWindows(
	context.Context,
	*application.ApplicationSyncWindowsQuery,
	...grpc.CallOption,
) (*application.ApplicationSyncWindowsResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) RevisionMetadata(context.Context, *application.RevisionMetadataQuery, ...grpc.CallOption) (*argoCDV1Alpha1.RevisionMetadata, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) RevisionChartDetails(context.Context, *application.RevisionMetadataQuery, ...grpc.CallOption) (*argoCDV1Alpha1.ChartDetails, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) GetManifests(context.Context, *application.ApplicationManifestQuery, ...grpc.CallOption) (*repoApiclient.ManifestResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) GetManifestsWithFiles(context.Context, ...grpc.CallOption) (application.ApplicationService_GetManifestsWithFilesClient, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) Update(context.Context, *application.ApplicationUpdateRequest, ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) UpdateSpec(context.Context, *application.ApplicationUpdateSpecRequest, ...grpc.CallOption) (*argoCDV1Alpha1.ApplicationSpec, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) Patch(context.Context, *application.ApplicationPatchRequest, ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) Delete(context.Context, *application.ApplicationDeleteRequest, ...grpc.CallOption) (*application.ApplicationResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) ManagedResources(context.Context, *application.ResourcesQuery, ...grpc.CallOption) (*application.ManagedResourcesResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) ResourceTree(context.Context, *application.ResourcesQuery, ...grpc.CallOption) (*argoCDV1Alpha1.ApplicationTree, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) WatchResourceTree(context.Context, *application.ResourcesQuery, ...grpc.CallOption) (application.ApplicationService_WatchResourceTreeClient, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) Rollback(context.Context, *application.ApplicationRollbackRequest, ...grpc.CallOption) (*argoCDV1Alpha1.Application, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) TerminateOperation(context.Context, *application.OperationTerminateRequest, ...grpc.CallOption) (*application.OperationTerminateResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) GetResource(context.Context, *application.ApplicationResourceRequest, ...grpc.CallOption) (*application.ApplicationResourceResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) PatchResource(context.Context, *application.ApplicationResourcePatchRequest, ...grpc.CallOption) (*application.ApplicationResourceResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) ListResourceActions(context.Context, *application.ApplicationResourceRequest, ...grpc.CallOption) (*application.ResourceActionsListResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) RunResourceAction(context.Context, *application.ResourceActionRunRequest, ...grpc.CallOption) (*application.ApplicationResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) DeleteResource(context.Context, *application.ApplicationResourceDeleteRequest, ...grpc.CallOption) (*application.ApplicationResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) PodLogs(context.Context, *application.ApplicationPodLogsQuery, ...grpc.CallOption) (application.ApplicationService_PodLogsClient, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) ListLinks(context.Context, *application.ListAppLinksRequest, ...grpc.CallOption) (*application.LinksResponse, error) {
	return nil, nil
}

func (f *fakeFullApplicationServiceClient) ListResourceLinks(context.Context, *application.ApplicationResourceRequest, ...grpc.CallOption) (*application.LinksResponse, error) {
	return nil, nil
}

func TestNewGlobalArgoCDAppManager(t *testing.T) {
	// Mutates globals.ArgoCDApplicationClient — sequential only.

	fakeClient := &fakeFullApplicationServiceClient{}

	tests := []struct {
		name          string
		globalClient  application.ApplicationServiceClient
		wantNilClient bool
	}{
		{
			name:          "nil global client yields nil manager client",
			globalClient:  nil,
			wantNilClient: true,
		},
		{
			name:         "non-nil global client is forwarded to manager",
			globalClient: fakeClient,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := globals.ArgoCDApplicationClient
			t.Cleanup(func() { globals.ArgoCDApplicationClient = orig })
			globals.ArgoCDApplicationClient = tc.globalClient

			mgr := newGlobalArgoCDAppManager()

			if tc.wantNilClient {
				assert.Nil(t, mgr.client)
			} else {
				assert.Equal(t, tc.globalClient, mgr.client)
			}
			assert.NotNil(t, mgr.reconnect)
		})
	}
}

func TestSyncAllArgoCDApps(t *testing.T) {
	tests := []struct {
		name          string
		cloudProvider string
		listResponse  *argoCDV1Alpha1.ApplicationList
		listErr       error
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "lists and skips all synced apps",
			listResponse: &argoCDV1Alpha1.ApplicationList{
				Items: []argoCDV1Alpha1.Application{
					{ObjectMeta: metaV1.ObjectMeta{Name: "app-a"}},
					{ObjectMeta: metaV1.ObjectMeta{Name: "app-b"}},
				},
			},
		},
		{
			name:          "AWS provider is no-op for CSI",
			cloudProvider: constants.CloudProviderAWS,
			listResponse:  &argoCDV1Alpha1.ApplicationList{Items: nil},
		},
		{
			name:          "list failure returns error",
			listErr:       errors.New("connection refused"),
			wantErr:       true,
			wantErrSubstr: "failed listing ArgoCD apps",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cloudProvider != "" {
				orig := globals.CloudProviderName
				t.Cleanup(func() { globals.CloudProviderName = orig })
				globals.CloudProviderName = tc.cloudProvider
			}

			fakeClient := &fakeArgoCDAppClient{
				listResponse: tc.listResponse,
				listErr:      tc.listErr,
				getResponses: []fakeGetResponse{{app: syncedApp(), err: nil}},
			}
			mgr := NewArgoCDAppManager(fakeClient, nil)

			err := mgr.syncAllArgoCDApps(context.Background(), true)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSyncArgoCDApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		appName       string
		client        *fakeArgoCDAppClient
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:    "skips when already synced",
			appName: "my-app",
			client:  alwaysSynced(),
		},
		{
			name:    "kube-prometheus already synced does not panic",
			appName: constants.ArgoCDAppKubePrometheus,
			client:  alwaysSynced(),
		},
		{
			name:    "rook-ceph already synced does not panic",
			appName: constants.ArgoCDAppRookCeph,
			client:  alwaysSynced(),
		},
		{
			name:    "sync failure returns error",
			appName: "my-app",
			client: &fakeArgoCDAppClient{
				getResponses: []fakeGetResponse{
					{app: appWithOverallStatus(argoCDV1Alpha1.SyncStatusCodeOutOfSync), err: nil},
				},
				syncErr: errors.New("permission denied"),
			},
			wantErr:       true,
			wantErrSubstr: "failed syncing ArgoCD application",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mgr := NewArgoCDAppManager(tc.client, nil)
			err := mgr.syncArgoCDApp(context.Background(), tc.appName, nil)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestIsArgoCDAppSynced(t *testing.T) {
	tests := []struct {
		name           string
		appName        string
		resources      []*argoCDV1Alpha1.SyncOperationResource
		getResponse    *argoCDV1Alpha1.Application
		wantSynced     bool
		skipShort      bool
		setupReconnect bool
	}{
		{
			name:        "whole-app synced returns true",
			appName:     "my-app",
			getResponse: appWithOverallStatus(argoCDV1Alpha1.SyncStatusCodeSynced),
			wantSynced:  true,
		},
		{
			name:        "whole-app out-of-sync returns false",
			appName:     "my-app",
			getResponse: appWithOverallStatus(argoCDV1Alpha1.SyncStatusCodeOutOfSync),
			wantSynced:  false,
		},
		{
			name:        "whole-app unknown status returns false",
			appName:     "my-app",
			getResponse: appWithOverallStatus(argoCDV1Alpha1.SyncStatusCodeUnknown),
			wantSynced:  false,
		},
		{
			name:    "specified resource is synced returns true",
			appName: "my-app",
			resources: []*argoCDV1Alpha1.SyncOperationResource{
				{Group: "apps", Kind: "Deployment", Name: "my-deploy"},
			},
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{
					Group:  "apps",
					Kind:   "Deployment",
					Name:   "my-deploy",
					Status: argoCDV1Alpha1.SyncStatusCodeSynced,
				},
			}),
			wantSynced: true,
		},
		{
			name:    "specified resource is out-of-sync returns false",
			appName: "my-app",
			resources: []*argoCDV1Alpha1.SyncOperationResource{
				{Group: "apps", Kind: "Deployment", Name: "my-deploy"},
			},
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{
					Group:  "apps",
					Kind:   "Deployment",
					Name:   "my-deploy",
					Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync,
				},
			}),
			wantSynced: false,
		},
		{
			name:    "specified resource absent from status returns false",
			appName: "my-app",
			resources: []*argoCDV1Alpha1.SyncOperationResource{
				{Group: "apps", Kind: "Deployment", Name: "missing"},
			},
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{
					Group:  "apps",
					Kind:   "Deployment",
					Name:   "other",
					Status: argoCDV1Alpha1.SyncStatusCodeSynced,
				},
			}),
			wantSynced: false,
		},
		{
			name:    "all specified resources synced returns true",
			appName: "my-app",
			resources: []*argoCDV1Alpha1.SyncOperationResource{
				{Group: "apps", Kind: "Deployment", Name: "d1"},
				{Group: "", Kind: "Service", Name: "s1"},
			},
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{Group: "apps", Kind: "Deployment", Name: "d1", Status: argoCDV1Alpha1.SyncStatusCodeSynced},
				{Group: "", Kind: "Service", Name: "s1", Status: argoCDV1Alpha1.SyncStatusCodeSynced},
			}),
			wantSynced: true,
		},
		{
			name:    "one of multiple specified resources out-of-sync returns false",
			appName: "my-app",
			resources: []*argoCDV1Alpha1.SyncOperationResource{
				{Group: "apps", Kind: "Deployment", Name: "d1"},
				{Group: "", Kind: "Service", Name: "s1"},
			},
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{Group: "apps", Kind: "Deployment", Name: "d1", Status: argoCDV1Alpha1.SyncStatusCodeSynced},
				{Group: "", Kind: "Service", Name: "s1", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
			}),
			wantSynced: false,
		},
		{
			name:    "velero: non-Schedule non-Backup resources all synced returns true",
			appName: constants.ArgoCDAppVelero,
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{Kind: "Deployment", Status: argoCDV1Alpha1.SyncStatusCodeSynced},
				{Kind: "Service", Status: argoCDV1Alpha1.SyncStatusCodeSynced},
				{Kind: "Schedule", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
				{Kind: "Backup", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
			}),
			wantSynced: true,
		},
		{
			name:    "velero: a non-Schedule non-Backup resource out-of-sync returns false",
			appName: constants.ArgoCDAppVelero,
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{Kind: "Deployment", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
				{Kind: "Schedule", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
			}),
			wantSynced: false,
		},
		{
			name:    "velero: only Schedule and Backup resources present returns true",
			appName: constants.ArgoCDAppVelero,
			getResponse: appWithResourceStatuses([]argoCDV1Alpha1.ResourceStatus{
				{Kind: "Schedule", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
				{Kind: "Backup", Status: argoCDV1Alpha1.SyncStatusCodeOutOfSync},
			}),
			wantSynced: true,
		},
		{
			name:        "velero: empty resource list returns true",
			appName:     constants.ArgoCDAppVelero,
			getResponse: appWithResourceStatuses(nil),
			wantSynced:  true,
		},
		{
			name:           "reconnect called on Get error then succeeds",
			appName:        "my-app",
			skipShort:      true,
			setupReconnect: true,
			wantSynced:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipShort && testing.Short() {
				t.Skip("requires 10s sleep in production retry loop")
			}

			if tc.setupReconnect {
				failClient := &fakeArgoCDAppClient{
					getResponses: []fakeGetResponse{{app: nil, err: errors.New("connection refused")}},
				}
				successClient := &fakeArgoCDAppClient{
					getResponses: []fakeGetResponse{{app: syncedApp(), err: nil}},
				}
				reconnectCalled := 0
				mgr := NewArgoCDAppManager(failClient, nil)
				mgr.reconnect = func(_ context.Context) {
					reconnectCalled++
					mgr.client = successClient
				}
				got := mgr.isArgoCDAppSynced(context.Background(), tc.appName, tc.resources)
				require.Equal(t, 1, reconnectCalled)
				assert.Equal(t, tc.wantSynced, got)
				return
			}

			fakeClient := &fakeArgoCDAppClient{
				getResponses: []fakeGetResponse{{app: tc.getResponse, err: nil}},
			}
			mgr := NewArgoCDAppManager(fakeClient, nil)
			got := mgr.isArgoCDAppSynced(context.Background(), tc.appName, tc.resources)
			assert.Equal(t, tc.wantSynced, got)
		})
	}
}

func TestGetKubeAidAgentRolePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource string
		action   string
		effect   string
		want     string
	}{
		{
			name:     "allow applications get",
			resource: "applications",
			action:   "get",
			effect:   constants.ArgoCDRBACEffectAllow,
			want:     "p, proj:kubeaid:kubeaid-agent, applications, get, kubeaid/*, allow",
		},
		{
			name:     "allow applications sync",
			resource: "applications",
			action:   "sync",
			effect:   constants.ArgoCDRBACEffectAllow,
			want:     "p, proj:kubeaid:kubeaid-agent, applications, sync, kubeaid/*, allow",
		},
		{
			name:     "deny applications get",
			resource: "applications",
			action:   "get",
			effect:   constants.ArgoCDRBACEffectDeny,
			want:     "p, proj:kubeaid:kubeaid-agent, applications, get, kubeaid/*, deny",
		},
		{
			name:     "allow repositories get",
			resource: "repositories",
			action:   "get",
			effect:   constants.ArgoCDRBACEffectAllow,
			want:     "p, proj:kubeaid:kubeaid-agent, repositories, get, kubeaid/*, allow",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := getKubeAidAgentRolePolicy(tc.resource, tc.action, tc.effect)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetArgoCDAdminPassword(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme(t)

	tests := []struct {
		name          string
		secret        *coreV1.Secret
		wantPass      string
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name: "returns password bytes as string",
			secret: &coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "argocd-initial-admin-secret",
					Namespace: constants.NamespaceArgoCD,
				},
				Data: map[string][]byte{"password": []byte("super-secret")},
			},
			wantPass: "super-secret",
		},
		{
			name: "returns empty string when password key is absent",
			secret: &coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "argocd-initial-admin-secret",
					Namespace: constants.NamespaceArgoCD,
				},
				Data: map[string][]byte{},
			},
			wantPass: "",
		},
		{
			name: "returns password with special characters intact",
			secret: &coreV1.Secret{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "argocd-initial-admin-secret",
					Namespace: constants.NamespaceArgoCD,
				},
				Data: map[string][]byte{"password": []byte("P@$$w0rd!#")},
			},
			wantPass: "P@$$w0rd!#",
		},
		{
			name:          "returns error when secret does not exist",
			secret:        nil,
			wantErr:       true,
			wantErrSubstr: "failed getting argocd-initial-admin-secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			builder := crFake.NewClientBuilder().WithScheme(scheme)
			if tc.secret != nil {
				builder = builder.WithObjects(tc.secret)
			}
			fakeClient := builder.Build()

			got, err := getArgoCDAdminPassword(context.Background(), fakeClient)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPass, got)
		})
	}
}

func TestArgoCDHelmValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(dir string)
		wantNil    bool
		wantSuffix string
	}{
		{
			name:    "returns nil when values-argocd.yaml does not exist",
			setup:   func(_ string) {},
			wantNil: true,
		},
		{
			name: "returns ValueFiles pointing at the rendered file when present",
			setup: func(dir string) {
				sub := filepath.Join(dir, "argocd-apps")
				require.NoError(t, os.MkdirAll(sub, 0o750))
				require.NoError(t, os.WriteFile(
					filepath.Join(sub, "values-argocd.yaml"),
					[]byte(
						"---\nconfigs:\n  ssh:\n    knownHosts: |\n      gitea.example.com ssh-ed25519 AAA\n",
					),
					0o600,
				))
			},
			wantSuffix: "argocd-apps/values-argocd.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tc.setup(dir)

			got := argoCDHelmValues(context.Background(), dir)
			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			want := filepath.Join(dir, tc.wantSuffix)
			require.Len(t, got.ValueFiles, 1)
			assert.Equal(t, want, got.ValueFiles[0])
		})
	}
}
