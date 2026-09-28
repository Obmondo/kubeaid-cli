// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	storageV1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// Names the cluster tests use in more than one place.
const (
	nsCentral   = "security-operations"
	nsTenant1   = "wazuh-001"
	tenant1     = "001"
	indexerName = "wazuh-001-indexer"
	backupList  = "BackupList"

	fieldStatus   = "status"
	fieldSchedule = "schedule"
)

// allocatable builds a node's allocatable resources without an enum map
// literal (the exhaustive linter wants every ResourceName in one).
func allocatable(cpu, memory string) coreV1.ResourceList {
	rl := coreV1.ResourceList{}
	rl[coreV1.ResourceCPU] = resource.MustParse(cpu)
	rl[coreV1.ResourceMemory] = resource.MustParse(memory)
	return rl
}

func testSOCConfig() *config.SecurityOperationsConfig {
	return &config.SecurityOperationsConfig{
		Enabled:       true,
		Domain:        "example.com",
		HostPrefix:    "soc-",
		AgentHost:     "agents.example.com",
		ClusterIssuer: "letsencrypt-prod",
		Keycloak:      config.SecurityOperationsKeycloakConfig{URL: "https://keycloak.example.com/auth"},
		Tenants: []config.SecurityOperationsTenant{
			{Code: tenant1, Name: "A", IndexerReplicas: 1},
			{Code: "002", Name: "B", IndexerReplicas: 2},
		},
	}
}

func runningPod(ns, name string, labels map[string]string, ready bool) *coreV1.Pod {
	status := coreV1.ConditionFalse
	if ready {
		status = coreV1.ConditionTrue
	}
	return &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{Namespace: ns, Name: name, Labels: labels},
		Status: coreV1.PodStatus{
			Phase:      coreV1.PodRunning,
			Conditions: []coreV1.PodCondition{{Type: coreV1.PodReady, Status: status}},
		},
	}
}

func dynClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	gvrs := map[schema.GroupVersionResource]string{
		clusterIssuerGVR:       "ClusterIssuerList",
		ApplicationGVR:         kindApplication + "List",
		VeleroBackupGVR:        backupList,
		VeleroRestoreGVR:       "RestoreList",
		VeleroScheduleGVR:      "ScheduleList",
		CNPGClusterGVR:         "ClusterList",
		CNPGBackupGVR:          backupList,
		CNPGScheduledBackupGVR: "ScheduledBackupList",
		MariaDBGVR:             "MariaDBList",
		MariaDBBackupGVR:       backupList,
		MariaDBRestoreGVR:      "RestoreList",
		CronJobGVR:             "CronJobList",
		JobGVR:                 "JobList",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), gvrs, objs...)
}

func obj(apiVersion, kind, ns, name string, labels map[string]string, fields map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind}}
	u.SetNamespace(ns)
	u.SetName(name)
	if labels != nil {
		u.SetLabels(labels)
	}
	for k, v := range fields {
		u.Object[k] = v
	}
	return u
}

func TestPreflight(t *testing.T) {
	kube := fake.NewSimpleClientset(
		runningPod("argocd", "argocd-application-controller-0", map[string]string{labelAppName: "argocd-application-controller"}, true),
		runningPod(sealedSecretsName, "ss", map[string]string{labelAppName: sealedSecretsName}, true),
		runningPod("cert-manager", "cm", map[string]string{labelAppName: "cert-manager"}, true),
		runningPod("cnpg-system", "cnpg", map[string]string{labelAppName: "cloudnative-pg"}, true),
		&storageV1.StorageClass{ObjectMeta: metaV1.ObjectMeta{
			Name:        "local",
			Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
		}},
		&coreV1.Node{ObjectMeta: metaV1.ObjectMeta{Name: "n1"}, Status: coreV1.NodeStatus{Allocatable: allocatable("16", "64Gi")}},
		&coreV1.Node{ObjectMeta: metaV1.ObjectMeta{Name: "cp"}, Spec: coreV1.NodeSpec{Taints: []coreV1.Taint{
			{Key: "node-role.kubernetes.io/control-plane", Effect: coreV1.TaintEffectNoSchedule},
		}}, Status: coreV1.NodeStatus{Allocatable: allocatable("64", "256Gi")}},
	)
	discovery, ok := kube.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok)
	discovery.Resources = []*metaV1.APIResourceList{
		{GroupVersion: "argoproj.io/v1alpha1", APIResources: []metaV1.APIResource{{Kind: kindApplication}}},
		{GroupVersion: "bitnami.com/v1alpha1", APIResources: []metaV1.APIResource{{Kind: kindSealedSecret}}},
		{GroupVersion: "cert-manager.io/v1", APIResources: []metaV1.APIResource{{Kind: "Certificate"}}},
		{GroupVersion: "postgresql.cnpg.io/v1", APIResources: []metaV1.APIResource{{Kind: "Cluster"}}},
		// mariadb-operator CRD present but no controller pod; Velero absent.
		{GroupVersion: "k8s.mariadb.com/v1alpha1", APIResources: []metaV1.APIResource{{Kind: "MariaDB"}}},
	}

	cfg := testSOCConfig()
	cfg.SharedStorageClass = "cephfs"
	lookup := func(_ context.Context, host string) ([]string, error) {
		if host == "soc-iris.example.com" {
			return nil, errors.New("no such host")
		}
		return []string{"192.0.2.1"}, nil
	}
	checks := RunPreflight(context.Background(), PreflightOptions{
		Kube:       kube,
		Dynamic:    dynClient(obj("cert-manager.io/v1", "ClusterIssuer", "", "letsencrypt-prod", nil, nil)),
		Config:     cfg,
		LookupHost: lookup,
	})
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	assert.Equal(t, CheckPass, byName["cluster reachable"].Status)
	assert.Equal(t, CheckPass, byName["controller argo-cd"].Status)
	assert.Equal(t, CheckWarn, byName["controller mariadb-operator"].Status, byName["controller mariadb-operator"].Detail)
	assert.Equal(t, CheckWarn, byName["controller velero"].Status)
	assert.Equal(t, CheckPass, byName["cluster issuer"].Status)
	assert.Equal(t, CheckPass, byName["default storage class"].Status)
	assert.Equal(t, CheckFail, byName["shared (RWX) storage class"].Status)
	assert.Equal(t, CheckWarn, byName["DNS"].Status)
	assert.Contains(t, byName["DNS"].Detail, "soc-iris.example.com")
	// Only the untainted node counts: 16 CPU < 8 + 2*3 replicas = 14 fits, 64 GiB >= 42.
	assert.Equal(t, CheckPass, byName["node capacity"].Status, byName["node capacity"].Detail)
	assert.Contains(t, byName["node capacity"].Detail, "1 schedulable node")

	assert.True(t, PreflightFailed(checks, false))
	var buf bytes.Buffer
	require.NoError(t, PrintChecks(&buf, checks))
	assert.Contains(t, buf.String(), "shared (RWX) storage class")

	// Velero required: a failure.
	checks = RunPreflight(context.Background(), PreflightOptions{Kube: kube, RequireVelero: true})
	for _, c := range checks {
		if c.Name == "controller velero" {
			assert.Equal(t, CheckFail, c.Status)
		}
	}
}

func TestRequiredCapacity(t *testing.T) {
	cfg := testSOCConfig()
	assert.Equal(t, Capacity{8 + 2 + 4, 24 + 6 + 12}, RequiredCapacity(cfg, ProfileStandard))
	cfg.AITriage.Enabled = true
	single := RequiredCapacity(cfg, ProfileSingle)
	assert.Equal(t, Capacity{3 + 1 + 2 + 2, 8 + 3 + 6 + 8}, single)
}

func TestConfiguredHosts(t *testing.T) {
	assert.Equal(t, []string{
		"soc-wazuh.example.com", "soc-iris.example.com", "soc-misp.example.com", "soc-velociraptor.example.com",
		"soc-wazuh-001.example.com", "soc-wazuh-002.example.com", "agents.example.com", "keycloak.example.com",
	}, ConfiguredHosts(testSOCConfig()))
}

func TestCollectStatus(t *testing.T) {
	now := metaV1.NewTime(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	older := metaV1.NewTime(now.Add(-time.Hour))
	kube := fake.NewSimpleClientset(
		runningPod(nsCentral, "iris-0", map[string]string{labelAppName: "dfir-iris", labelComponent: "app"}, true),
		runningPod(nsCentral, "iris-1", map[string]string{labelAppName: "dfir-iris", labelComponent: "app"}, false),
		runningPod(nsTenant1, "wazuh-001-indexer-0", map[string]string{labelApp: indexerName}, true),
		&coreV1.Pod{ObjectMeta: metaV1.ObjectMeta{
			Namespace: nsCentral, Name: "siem-reconciler-new-abc",
			Labels: map[string]string{"job-name": "siem-reconciler-new"},
		}, Status: coreV1.PodStatus{Phase: coreV1.PodSucceeded}},
		&batchV1.Job{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: nsCentral, Name: "siem-reconciler-old",
				CreationTimestamp: older, Labels: map[string]string{labelComponent: "reconciler"},
			},
			Status: batchV1.JobStatus{Conditions: []batchV1.JobCondition{{Type: batchV1.JobFailed, Status: coreV1.ConditionTrue, Reason: "BackoffLimitExceeded"}}},
		},
		&batchV1.Job{
			ObjectMeta: metaV1.ObjectMeta{
				Namespace: nsCentral, Name: "siem-reconciler-new",
				CreationTimestamp: now, Labels: map[string]string{labelComponent: "reconciler"},
			},
			Status: batchV1.JobStatus{CompletionTime: &now, Conditions: []batchV1.JobCondition{{Type: batchV1.JobComplete, Status: coreV1.ConditionTrue}}},
		},
		&appsV1.StatefulSet{
			ObjectMeta: metaV1.ObjectMeta{Namespace: nsTenant1, Name: indexerName},
			Spec:       appsV1.StatefulSetSpec{Replicas: ptr.To(int32(1))}, Status: appsV1.StatefulSetStatus{ReadyReplicas: 1},
		},
		&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{Namespace: nsTenant1, Name: EnrolmentBundleSecret},
			Data:       bundleData(),
		},
		&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{Namespace: "wazuh-002", Name: EnrolmentBundleSecret},
			Data:       map[string][]byte{BundleKeyAuthdPass: []byte("x")},
		},
	)
	dyn := dynClient(
		obj("argoproj.io/v1alpha1", kindApplication, "argocd", nsCentral, nil, map[string]any{
			fieldStatus: map[string]any{
				"sync":   map[string]any{fieldStatus: "Synced", "revision": "0123456789abcdef0123"},
				"health": map[string]any{fieldStatus: "Healthy"},
			},
		}),
	)
	st, err := CollectStatus(context.Background(), StatusOptions{
		Kube:    kube,
		Dynamic: dyn,
		Tenants: []string{tenant1, "002"},
		Logs: func(_ context.Context, ns, pod string) (string, error) {
			assert.Equal(t, "siem-reconciler-new-abc", pod)
			return "COMPONENT KIND NAME\nwazuh x y ok\n3 changes (applied)\n", nil
		},
		Agents: func(_ context.Context, ns string) (string, error) { return "2", nil },
	})
	require.NoError(t, err)

	apps := map[string]AppStatus{}
	for _, a := range st.Apps {
		apps[a.Name] = a
	}
	assert.Equal(t, "missing", apps["root"].Sync)
	assert.Equal(t, "Synced", apps[nsCentral].Sync)
	assert.Equal(t, "0123456789ab", apps[nsCentral].Revision)
	assert.Equal(t, "missing", apps["wazuh-002"].Sync)

	assert.Contains(t, st.Components, ComponentStatus{Namespace: nsCentral, Component: "dfir-iris/app", Ready: 1, Total: 2})
	assert.Contains(t, st.Components, ComponentStatus{Namespace: nsTenant1, Component: indexerName, Ready: 1, Total: 1})

	require.NotNil(t, st.Reconciler)
	assert.Equal(t, "siem-reconciler-new", st.Reconciler.Job)
	assert.Equal(t, "succeeded", st.Reconciler.Result)
	assert.Equal(t, "3 changes (applied)", st.Reconciler.Summary)

	require.Len(t, st.Tenants, 2)
	assert.True(t, st.Tenants[0].IndexerHealthy)
	assert.True(t, st.Tenants[0].BundlePresent)
	assert.Equal(t, "2", st.Tenants[0].AgentsConnected)
	assert.Equal(t, "missing", st.Tenants[1].Indexer)
	assert.False(t, st.Tenants[1].BundlePresent)
	assert.Contains(t, st.Tenants[1].BundleMissing, BundleKeyInstallLinux)

	assert.False(t, st.Healthy())
	var buf bytes.Buffer
	require.NoError(t, st.Print(&buf))
	assert.Contains(t, buf.String(), "siem-reconciler-new")
	// Secret values never show up.
	assert.NotContains(t, buf.String(), "s3cret")
}

func bundleData() map[string][]byte {
	d := map[string][]byte{}
	for _, k := range EnrolmentBundleKeys {
		d[k] = []byte("v")
	}
	d[BundleKeyAuthdPass] = []byte("s3cret")
	d[BundleKeyManagerHost] = []byte("agents.example.com")
	d[BundleKeyRegistrationPort] = []byte("20015")
	d[BundleKeyEventsPort] = []byte("20014")
	d[BundleKeyInstallLinux] = []byte("#!/bin/sh\nexport WAZUH_REGISTRATION_PASSWORD='s3cret'\n")
	d[BundleKeyVelociraptorInstall] = []byte("Download velociraptor\nRun it as a service\n")
	return d
}

// The reconciler's /status endpoint wins over the last Job, and its content
// package version is shown.
func TestCollectStatusFromReconcilerEndpoint(t *testing.T) {
	kube := fake.NewSimpleClientset()
	st, err := CollectStatus(context.Background(), StatusOptions{
		Kube: kube,
		ReconcilerStatusEndpoint: func(context.Context) ([]byte, error) {
			return []byte(`{"ok":true,"lastRun":"2026-09-01T10:00:00Z","changes":3,"errors":0,"contentVersion":"2026.09.1"}`), nil
		},
	})
	require.NoError(t, err)
	require.NotNil(t, st.Reconciler)
	assert.Equal(t, "endpoint", st.Reconciler.Source)
	assert.Equal(t, "succeeded", st.Reconciler.Result)
	assert.Equal(t, "2026-09-01T10:00:00Z", st.Reconciler.Finished)
	assert.Equal(t, "3 changes", st.Reconciler.Summary)
	assert.Equal(t, "2026.09.1", st.Reconciler.ContentVersion)
	var buf bytes.Buffer
	require.NoError(t, st.Print(&buf))
	assert.Contains(t, buf.String(), "content package")

	// An unreachable endpoint (no long-running reconciler) and no Job: nothing.
	st, err = CollectStatus(context.Background(), StatusOptions{
		Kube:                     kube,
		ReconcilerStatusEndpoint: func(context.Context) ([]byte, error) { return nil, errors.New("not found") },
	})
	require.NoError(t, err)
	assert.Nil(t, st.Reconciler)
}

func TestCountActiveAgents(t *testing.T) {
	out := `
Wazuh agent_control. List of available agents:
   ID: 000, Name: wazuh-001-manager-master-0 (server), IP: 127.0.0.1, Active/Local
   ID: 001, Name: host-a, IP: any, Active
   ID: 002, Name: host-b, IP: any, Disconnected
   ID: 003, Name: host-c, IP: any, Active

List of agentless devices:
`
	assert.Equal(t, 2, CountActiveAgents(out))
}

func TestEnrol(t *testing.T) {
	kube := fake.NewSimpleClientset(&coreV1.Secret{
		ObjectMeta: metaV1.ObjectMeta{Namespace: nsTenant1, Name: EnrolmentBundleSecret},
		Data:       bundleData(),
	})
	_, err := ReadEnrolmentBundle(context.Background(), kube, "002")
	assert.ErrorContains(t, err, "wazuh-002/enrolment-bundle")

	b, err := ReadEnrolmentBundle(context.Background(), kube, tenant1)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, b.WriteInstructions(&buf, OSLinux))
	out := buf.String()
	assert.Contains(t, out, "Wazuh manager agents.example.com, registration port 20015, events port 20014")
	assert.Contains(t, out, "sudo sh ./install-linux.sh")
	assert.Contains(t, out, "WAZUH_REGISTRATION_PASSWORD")
	assert.Contains(t, out, "# Run it as a service")

	buf.Reset()
	require.NoError(t, b.WriteInstructions(&buf, OSWindows))
	assert.Contains(t, buf.String(), "powershell -ExecutionPolicy Bypass")

	o, err := ParseEnrolOS("MacOS")
	require.NoError(t, err)
	assert.Equal(t, OSMacOS, o)
	_, err = ParseEnrolOS("bsd")
	assert.Error(t, err)

	dir := t.TempDir()
	written, err := b.WriteFiles(dir, OSLinux)
	require.NoError(t, err)
	assert.Len(t, written, 3)
}

func TestPlanBackupAndRestore(t *testing.T) {
	ctx := context.Background()
	dyn := dynClient(
		obj("velero.io/v1", "Schedule", "velero", "soc-daily", nil, map[string]any{fieldSpec: map[string]any{
			fieldSchedule: "0 2 * * *",
			"template":    map[string]any{"includedNamespaces": []any{nsCentral}, "ttl": "240h0m0s", "snapshotVolumes": true},
		}}),
		obj("postgresql.cnpg.io/v1", "Cluster", nsCentral, "iris-pg", nil, map[string]any{fieldSpec: map[string]any{
			"backup": map[string]any{"barmanObjectStore": map[string]any{"destinationPath": "s3://bucket"}},
		}}),
		obj("postgresql.cnpg.io/v1", "Cluster", nsCentral, "no-backup-pg", nil, map[string]any{fieldSpec: map[string]any{}}),
		obj("postgresql.cnpg.io/v1", "ScheduledBackup", nsCentral, "iris-pg-daily", nil, map[string]any{fieldSpec: map[string]any{
			"cluster": map[string]any{fieldName: "iris-pg"}, "method": "barmanObjectStore", fieldSchedule: "0 0 3 * * *",
		}}),
		obj("k8s.mariadb.com/v1alpha1", "MariaDB", nsCentral, "misp-db", nil, nil),
		obj("k8s.mariadb.com/v1alpha1", "Backup", nsCentral, "misp-db-scheduled", nil, map[string]any{fieldSpec: map[string]any{
			"mariaDbRef":  map[string]any{fieldName: "misp-db"},
			fieldSchedule: map[string]any{"cron": "0 1 * * *"},
			"storage":     map[string]any{"s3": map[string]any{"bucket": "b"}},
		}}),
		obj("batch/v1", "CronJob", nsTenant1, "indexer-snapshot", map[string]string{"kubesoc.io/backup": "opensearch-snapshot"},
			map[string]any{fieldSpec: map[string]any{"jobTemplate": map[string]any{fieldSpec: map[string]any{"backoffLimit": int64(0)}}}}),
	)
	plan, err := PlanBackup(ctx, BackupOptions{
		Dynamic: dyn,
		Tenants: []string{tenant1},
		Now:     time.Date(2026, 9, 1, 2, 3, 4, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Equal(t, "kubesoc-20260901-020304", plan.Name)

	kinds := map[string]*unstructured.Unstructured{}
	for _, o := range plan.Objects {
		kinds[o.Object.GetKind()+"/"+o.Object.GetName()] = o.Object
	}
	require.Contains(t, kinds, "Backup/kubesoc-20260901-020304")
	velero := kinds["Backup/kubesoc-20260901-020304"]
	ttl, _, _ := unstructured.NestedString(velero.Object, fieldSpec, "ttl")
	assert.Equal(t, "240h0m0s", ttl, "copied from the Schedule template")
	ns, _, _ := unstructured.NestedStringSlice(velero.Object, fieldSpec, "includedNamespaces")
	assert.Equal(t, []string{nsCentral, nsTenant1}, ns)

	cnpg := kinds["Backup/kubesoc-20260901-020304-iris-pg"]
	require.NotNil(t, cnpg)
	assert.Equal(t, "postgresql.cnpg.io/v1", cnpg.GetAPIVersion())
	method, _, _ := unstructured.NestedString(cnpg.Object, fieldSpec, "method")
	assert.Equal(t, "barmanObjectStore", method)
	assert.NotContains(t, kinds, "Backup/kubesoc-20260901-020304-no-backup-pg")

	mdb := kinds["Backup/kubesoc-20260901-020304-misp-db"]
	require.NotNil(t, mdb)
	_, hasSchedule, _ := unstructured.NestedFieldNoCopy(mdb.Object, fieldSpec, fieldSchedule)
	assert.False(t, hasSchedule)
	assert.Equal(t, "kubesoc-20260901-020304", mdb.GetLabels()[BackupSetLabel])

	assert.Contains(t, kinds, "Job/kubesoc-20260901-020304-indexer-snapshot")
	assert.Len(t, plan.Notes, 1, plan.Notes)

	require.NoError(t, plan.Execute(ctx, dyn))
	_, err = dyn.Resource(VeleroBackupGVR).Namespace("velero").Get(ctx, "kubesoc-20260901-020304", metaV1.GetOptions{})
	require.NoError(t, err)

	restore, err := PlanRestore(ctx, RestoreOptions{
		Dynamic: dyn, Backup: "kubesoc-20260901-020304",
		Now: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	rkinds := map[string]*unstructured.Unstructured{}
	for _, o := range restore.Objects {
		rkinds[o.Object.GetKind()] = o.Object
	}
	require.Contains(t, rkinds, "Restore")
	var mariaRestore, veleroRestore bool
	for _, o := range restore.Objects {
		switch o.GVR {
		case MariaDBRestoreGVR:
			mariaRestore = true
			ref, _, _ := unstructured.NestedString(o.Object.Object, fieldSpec, "backupRef", fieldName)
			assert.Equal(t, "kubesoc-20260901-020304-misp-db", ref)
		case VeleroRestoreGVR:
			veleroRestore = true
			policy, _, _ := unstructured.NestedString(o.Object.Object, fieldSpec, "existingResourcePolicy")
			assert.Equal(t, "none", policy)
		}
	}
	assert.True(t, mariaRestore)
	assert.True(t, veleroRestore)
	assert.NotEmpty(t, restore.Notes)
	var buf bytes.Buffer
	restore.Print(&buf)
	assert.Contains(t, buf.String(), "CNPG restores into a new cluster")

	_, err = PlanBackup(ctx, BackupOptions{Dynamic: dyn, VeleroSchedule: "nope"})
	assert.Error(t, err)
}
