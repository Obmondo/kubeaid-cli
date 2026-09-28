// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// EnrolmentBundleSecret is the Secret the reconciler writes into each tenant
// namespace, and the keys it holds.
const EnrolmentBundleSecret = "enrolment-bundle"

// Enrolment bundle keys (pkg/siem/enrolment).
const (
	BundleKeyAuthdPass           = "authd.pass"
	BundleKeyManagerHost         = "manager_host"
	BundleKeyRegistrationPort    = "registration_port"
	BundleKeyEventsPort          = "events_port"
	BundleKeyInstallLinux        = "install-linux.sh"
	BundleKeyInstallMacOS        = "install-macos.sh"
	BundleKeyInstallWindows      = "install-windows.ps1"
	BundleKeyVelociraptorConfig  = "velociraptor-client.config.yaml"
	BundleKeyVelociraptorInstall = "install-velociraptor.txt"
)

// EnrolmentBundleKeys are the keys a complete bundle has.
var EnrolmentBundleKeys = []string{
	BundleKeyAuthdPass, BundleKeyManagerHost, BundleKeyRegistrationPort, BundleKeyEventsPort,
	BundleKeyInstallLinux, BundleKeyInstallMacOS, BundleKeyInstallWindows,
	BundleKeyVelociraptorConfig, BundleKeyVelociraptorInstall,
}

// resultUnknown is the result of a Job or endpoint that says nothing useful.
const resultUnknown = "unknown"

// ApplicationGVR is Argo CD's Application resource.
var ApplicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: versionV1Alpha1, Resource: "applications"}

// AppStatus is one Argo CD Application's state.
type AppStatus struct {
	Name     string
	Sync     string
	Health   string
	Revision string
}

// ComponentStatus counts the pods of one component.
type ComponentStatus struct {
	Namespace string
	Component string
	Ready     int
	Total     int
}

// ReconcilerStatus is the reconciler's last result: from its /status endpoint
// when it runs long-running (Service siem-reconciler-metrics), else from the
// last CronJob Job and its pod log.
type ReconcilerStatus struct {
	// Source is "endpoint" or "job".
	Source   string
	Job      string
	Result   string
	Finished string
	// Summary is the reconciler's final "N changes" line, when logs could be read.
	Summary string
	// ContentVersion is the detection content package the reconciler applied,
	// when its /status reports one (securityOperations.content).
	ContentVersion string
}

// reconcilerStatusPayload is the part of the reconciler's /status JSON this
// reads. Unknown fields are ignored, so the endpoint may grow freely.
type reconcilerStatusPayload struct {
	LastRun   string `json:"lastRun"`
	Finished  string `json:"finished"`
	Result    string `json:"result"`
	OK        *bool  `json:"ok"`
	Changes   *int   `json:"changes"`
	Errors    *int   `json:"errors"`
	Summary   string `json:"summary"`
	Content   string `json:"contentVersion"`
	Content2  string `json:"content_version"`
	Component string `json:"component"`
}

// TenantStatus is one tenant's health.
type TenantStatus struct {
	Code            string
	Indexer         string
	IndexerHealthy  bool
	BundlePresent   bool
	BundleMissing   []string
	AgentsConnected string
}

// Status is everything `siem status` shows.
type Status struct {
	Apps       []AppStatus
	Components []ComponentStatus
	Reconciler *ReconcilerStatus
	Tenants    []TenantStatus
}

// StatusOptions are the inputs of CollectStatus.
type StatusOptions struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
	Tenants []string
	// Logs returns the tail of a pod's log; nil skips the reconciler summary.
	Logs func(ctx context.Context, namespace, pod string) (string, error)
	// Agents returns the number of connected agents of a tenant's manager
	// (read-only, e.g. agent_control through pods/exec); nil skips it.
	Agents func(ctx context.Context, namespace string) (string, error)
	// ReconcilerStatusEndpoint GETs the reconciler's /status JSON (the
	// long-running mode's endpoint behind Service siem-reconciler-metrics).
	// nil, or an error, falls back to the last Job and its log.
	ReconcilerStatusEndpoint func(ctx context.Context) ([]byte, error)
}

// CollectStatus reads the stack's state. It only reads.
func CollectStatus(ctx context.Context, opts StatusOptions) (*Status, error) {
	st := &Status{}

	apps := []string{constants.ArgoCDAppRoot, constants.ArgoCDAppSealedSecrets, constants.ArgoCDAppSecurityOperations}
	namespaces := []string{constants.NamespaceSecurityOperations}
	for _, code := range opts.Tenants {
		apps = append(apps, constants.SecurityOperationsTenantNamespacePrefix+code)
		namespaces = append(namespaces, constants.SecurityOperationsTenantNamespacePrefix+code)
	}
	for _, name := range apps {
		st.Apps = append(st.Apps, appStatus(ctx, opts.Dynamic, name))
	}

	for _, ns := range namespaces {
		comps, err := componentStatus(ctx, opts.Kube, ns)
		if err != nil {
			return nil, err
		}
		st.Components = append(st.Components, comps...)
	}

	st.Reconciler = reconcilerStatus(ctx, opts)

	for _, code := range opts.Tenants {
		st.Tenants = append(st.Tenants, tenantStatus(ctx, opts, code))
	}
	return st, nil
}

func appStatus(ctx context.Context, dyn dynamic.Interface, name string) AppStatus {
	s := AppStatus{Name: name}
	if dyn == nil {
		s.Sync = resultUnknown
		return s
	}
	app, err := dyn.Resource(ApplicationGVR).Namespace(constants.NamespaceArgoCD).Get(ctx, name, metaV1.GetOptions{})
	if apiErrors.IsNotFound(err) {
		s.Sync = "missing"
		return s
	}
	if err != nil {
		s.Sync = "error: " + err.Error()
		return s
	}
	s.Sync, _, _ = unstructured.NestedString(app.Object, "status", "sync", "status")
	s.Health, _, _ = unstructured.NestedString(app.Object, "status", "health", "status")
	rev, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	if len(rev) > 12 {
		rev = rev[:12]
	}
	s.Revision = rev
	return s
}

// The labels a component's pods are named by.
const (
	labelAppName   = "app.kubernetes.io/name"
	labelApp       = "app"
	labelComponent = "app.kubernetes.io/component"
)

// componentOf names the component of a pod: the app.kubernetes.io/name label,
// then app, then the pod's owner name.
func componentOf(pod coreV1.Pod) string {
	for _, l := range []string{labelAppName, labelApp} {
		if v := pod.Labels[l]; v != "" {
			if c := pod.Labels[labelComponent]; c != "" && c != v {
				return v + "/" + c
			}
			return v
		}
	}
	if len(pod.OwnerReferences) > 0 {
		return pod.OwnerReferences[0].Name
	}
	return pod.Name
}

func componentStatus(ctx context.Context, kube kubernetes.Interface, ns string) ([]ComponentStatus, error) {
	pods, err := kube.CoreV1().Pods(ns).List(ctx, metaV1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods in %s: %w", ns, err)
	}
	byComp := map[string]*ComponentStatus{}
	for _, pod := range pods.Items {
		// Finished Job pods are the reconciler's history, not components.
		if pod.Status.Phase == coreV1.PodSucceeded || pod.Status.Phase == coreV1.PodFailed {
			continue
		}
		name := componentOf(pod)
		c := byComp[name]
		if c == nil {
			c = &ComponentStatus{Namespace: ns, Component: name}
			byComp[name] = c
		}
		c.Total++
		if podReady(pod) {
			c.Ready++
		}
	}
	out := make([]ComponentStatus, 0, len(byComp))
	for _, c := range byComp {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out, nil
}

func podReady(pod coreV1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == coreV1.PodReady {
			return c.Status == coreV1.ConditionTrue
		}
	}
	return false
}

// ReconcilerJobSelector selects the siem-reconciler CronJob's Jobs and the
// Argo CD hook Jobs.
const ReconcilerJobSelector = "app.kubernetes.io/component=reconciler"

func reconcilerStatus(ctx context.Context, opts StatusOptions) *ReconcilerStatus {
	if rs := reconcilerStatusFromEndpoint(ctx, opts); rs != nil {
		return rs
	}
	jobs, err := opts.Kube.BatchV1().Jobs(constants.NamespaceSecurityOperations).List(ctx,
		metaV1.ListOptions{LabelSelector: ReconcilerJobSelector})
	if err != nil || len(jobs.Items) == 0 {
		return nil
	}
	sort.Slice(jobs.Items, func(i, j int) bool {
		return jobs.Items[i].CreationTimestamp.After(jobs.Items[j].CreationTimestamp.Time)
	})
	job := jobs.Items[0]
	rs := &ReconcilerStatus{Source: "job", Job: job.Name, Result: jobResult(job)}
	if job.Status.CompletionTime != nil {
		rs.Finished = job.Status.CompletionTime.UTC().Format(time.RFC3339)
	}
	if opts.Logs != nil {
		pods, err := opts.Kube.CoreV1().Pods(constants.NamespaceSecurityOperations).List(ctx,
			metaV1.ListOptions{LabelSelector: "job-name=" + job.Name})
		if err == nil && len(pods.Items) > 0 {
			if logs, err := opts.Logs(ctx, constants.NamespaceSecurityOperations, pods.Items[0].Name); err == nil {
				rs.Summary = summaryLine(logs)
			}
		}
	}
	return rs
}

// reconcilerStatusFromEndpoint reads the reconciler's /status JSON. nil when
// there is no endpoint, it cannot be reached, or it is not JSON.
func reconcilerStatusFromEndpoint(ctx context.Context, opts StatusOptions) *ReconcilerStatus {
	if opts.ReconcilerStatusEndpoint == nil {
		return nil
	}
	raw, err := opts.ReconcilerStatusEndpoint(ctx)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var p reconcilerStatusPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	rs := &ReconcilerStatus{
		Source:         "endpoint",
		Job:            "(long-running)",
		Result:         p.Result,
		Finished:       firstNonEmpty(p.Finished, p.LastRun),
		Summary:        p.Summary,
		ContentVersion: firstNonEmpty(p.Content, p.Content2),
	}
	if rs.Result == "" && p.OK != nil {
		rs.Result = "succeeded"
		if !*p.OK {
			rs.Result = "failed"
		}
	}
	if rs.Result == "" {
		rs.Result = resultUnknown
	}
	if rs.Summary == "" && p.Changes != nil {
		rs.Summary = fmt.Sprintf("%d changes", *p.Changes)
		if p.Errors != nil && *p.Errors > 0 {
			rs.Summary += fmt.Sprintf(", %d errors", *p.Errors)
		}
	}
	return rs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func jobResult(job batchV1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Status != coreV1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchV1.JobComplete:
			return "succeeded"
		case batchV1.JobFailed:
			return "failed: " + c.Reason
		case batchV1.JobSuspended, batchV1.JobFailureTarget, batchV1.JobSuccessCriteriaMet:
			// Not terminal: the Active count below says whether it still runs.
		}
	}
	if job.Status.Active > 0 {
		return "running"
	}
	return resultUnknown
}

// summaryLine returns the reconciler's last "N changes" line.
func summaryLine(logs string) string {
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "changes") {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

func tenantStatus(ctx context.Context, opts StatusOptions, code string) TenantStatus {
	ns := constants.SecurityOperationsTenantNamespacePrefix + code
	ts := TenantStatus{Code: code, Indexer: "missing"}

	sts, err := opts.Kube.AppsV1().StatefulSets(ns).Get(ctx, ns+"-indexer", metaV1.GetOptions{})
	if err == nil {
		want := int32(1)
		if sts.Spec.Replicas != nil {
			want = *sts.Spec.Replicas
		}
		ts.Indexer = fmt.Sprintf("%d/%d ready", sts.Status.ReadyReplicas, want)
		ts.IndexerHealthy = sts.Status.ReadyReplicas >= want && want > 0
	}

	secret, err := opts.Kube.CoreV1().Secrets(ns).Get(ctx, EnrolmentBundleSecret, metaV1.GetOptions{})
	if err == nil {
		// Only key names are looked at, never values.
		for _, k := range EnrolmentBundleKeys {
			if _, ok := secret.Data[k]; !ok {
				ts.BundleMissing = append(ts.BundleMissing, k)
			}
		}
		ts.BundlePresent = len(ts.BundleMissing) == 0
	} else {
		ts.BundleMissing = []string{"(Secret " + EnrolmentBundleSecret + ")"}
	}

	ts.AgentsConnected = "-"
	if opts.Agents != nil {
		if n, err := opts.Agents(ctx, ns); err == nil {
			ts.AgentsConnected = n
		} else {
			ts.AgentsConnected = resultUnknown
		}
	}
	return ts
}

// CountActiveAgents counts the agents `agent_control -l` lists as Active
// (the manager itself, ID 000, excluded).
func CountActiveAgents(agentControlOutput string) int {
	n := 0
	for _, line := range strings.Split(agentControlOutput, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ID:") || strings.HasPrefix(line, "ID: 000") {
			continue
		}
		if strings.HasSuffix(line, "Active") || strings.Contains(line, ", Active") {
			n++
		}
	}
	return n
}

// Healthy reports whether every Application is Synced and Healthy, every
// component pod is ready and every tenant has a healthy indexer and bundle.
func (s *Status) Healthy() bool {
	for _, a := range s.Apps {
		if a.Sync != "Synced" || a.Health != "Healthy" {
			return false
		}
	}
	for _, c := range s.Components {
		if c.Ready < c.Total {
			return false
		}
	}
	for _, t := range s.Tenants {
		if !t.IndexerHealthy || !t.BundlePresent {
			return false
		}
	}
	return true
}

// Print writes the status as tables.
func (s *Status) Print(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "APPLICATION\tSYNC\tHEALTH\tREVISION")
	for _, a := range s.Apps {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, a.Sync, a.Health, a.Revision)
	}
	_, _ = fmt.Fprintln(tw)
	_, _ = fmt.Fprintln(tw, "NAMESPACE\tCOMPONENT\tREADY")
	for _, c := range s.Components {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d/%d\n", c.Namespace, c.Component, c.Ready, c.Total)
	}
	_, _ = fmt.Fprintln(tw)
	if s.Reconciler == nil {
		_, _ = fmt.Fprintln(tw, "RECONCILER\tno run found")
	} else {
		_, _ = fmt.Fprintln(tw, "RECONCILER\tRESULT\tFINISHED\tSUMMARY")
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Reconciler.Job, s.Reconciler.Result, s.Reconciler.Finished, s.Reconciler.Summary)
		if s.Reconciler.ContentVersion != "" {
			_, _ = fmt.Fprintf(tw, "content package\t%s\n", s.Reconciler.ContentVersion)
		}
	}
	_, _ = fmt.Fprintln(tw)
	_, _ = fmt.Fprintln(tw, "TENANT\tINDEXER\tENROLMENT BUNDLE\tAGENTS CONNECTED")
	for _, t := range s.Tenants {
		bundle := "present"
		if !t.BundlePresent {
			bundle = "missing " + strings.Join(t.BundleMissing, ",")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.Code, t.Indexer, bundle, t.AgentsConnected)
	}
	return tw.Flush()
}
