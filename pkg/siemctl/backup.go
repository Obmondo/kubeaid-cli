// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// The backup CR kinds a SOC backup is made of. They are driven generically
// (unstructured), so the stack needs no Go types of the operators.
//
// Nothing is looked up by name: the scheduled objects the security-operations
// chart renders are found by their kind and namespace and cloned with the
// schedule dropped, so renaming one in the chart needs no change here.
// The API groups and versions of the backup CRs.
const (
	groupVelero     = "velero.io"
	groupCNPG       = "postgresql.cnpg.io"
	groupMariaDB    = "k8s.mariadb.com"
	versionV1Alpha1 = "v1alpha1"

	resourceBackups = "backups"
)

var (
	VeleroBackupGVR   = schema.GroupVersionResource{Group: groupVelero, Version: "v1", Resource: resourceBackups}
	VeleroRestoreGVR  = schema.GroupVersionResource{Group: groupVelero, Version: "v1", Resource: "restores"}
	VeleroScheduleGVR = schema.GroupVersionResource{Group: groupVelero, Version: "v1", Resource: "schedules"}

	CNPGClusterGVR         = schema.GroupVersionResource{Group: groupCNPG, Version: "v1", Resource: "clusters"}
	CNPGBackupGVR          = schema.GroupVersionResource{Group: groupCNPG, Version: "v1", Resource: resourceBackups}
	CNPGScheduledBackupGVR = schema.GroupVersionResource{Group: groupCNPG, Version: "v1", Resource: "scheduledbackups"}

	MariaDBGVR        = schema.GroupVersionResource{Group: groupMariaDB, Version: versionV1Alpha1, Resource: "mariadbs"}
	MariaDBBackupGVR  = schema.GroupVersionResource{Group: groupMariaDB, Version: versionV1Alpha1, Resource: resourceBackups}
	MariaDBRestoreGVR = schema.GroupVersionResource{Group: groupMariaDB, Version: versionV1Alpha1, Resource: "restores"}

	CronJobGVR = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
	JobGVR     = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
)

// BackupSetLabel ties every object of one `siem backup` run together.
const BackupSetLabel = "kubesoc.io/backup-set"

// SnapshotCronJobLabel marks a CronJob that takes an OpenSearch snapshot;
// `siem backup` runs it once. The chart puts it on one CronJob per indexer
// namespace, and a run is idempotent (register repository, update the policy,
// snapshot, prune), so triggering it out of schedule is safe.
const SnapshotCronJobLabel = "kubesoc.io/backup=opensearch-snapshot"

// BackupOptions are the inputs of PlanBackup.
type BackupOptions struct {
	Dynamic dynamic.Interface
	// Tenants are the tenant codes; their namespaces are backed up with the
	// central one.
	Tenants []string
	// Name of the backup set; empty: kubesoc-<UTC timestamp>.
	Name string
	// VeleroSchedule, when set, is the Velero Schedule whose template the
	// Backup copies. Empty: the first Schedule that covers the SOC namespace,
	// else a plain Backup of the SOC namespaces.
	VeleroSchedule string
	// SkipVelero leaves out the Velero Backup (e.g. no Velero installed).
	SkipVelero bool
	Now        time.Time
}

// PlannedObject is one object a backup or restore creates.
type PlannedObject struct {
	GVR    schema.GroupVersionResource
	Object *unstructured.Unstructured
}

// Plan is what a backup or restore creates, and the steps it cannot do.
type Plan struct {
	Name    string
	Objects []PlannedObject
	Notes   []string
}

// SOCNamespaces returns the central namespace and each tenant's.
func SOCNamespaces(tenants []string) []string {
	ns := []string{constants.NamespaceSecurityOperations}
	for _, t := range tenants {
		ns = append(ns, constants.SecurityOperationsTenantNamespacePrefix+t)
	}
	return ns
}

// API groups and field names used by the backup plans.
const (
	apiVersionVelero  = "velero.io/v1"
	apiVersionCNPG    = groupCNPG + "/v1"
	apiVersionMariaDB = groupMariaDB + "/" + versionV1Alpha1
	apiVersionBatch   = "batch/v1"

	kindBackup  = "Backup"
	kindRestore = "Restore"
	kindJob     = "Job"

	fieldSpec = "spec"
	fieldName = "name"
)

// PlanBackup lists the objects of a SOC backup: one Velero Backup of the SOC
// namespaces, a CNPG Backup per PostgreSQL cluster, a MariaDB Backup per
// MariaDB (cloned from its scheduled Backup), and a Job per OpenSearch
// snapshot CronJob. It only reads.
func PlanBackup(ctx context.Context, opts BackupOptions) (*Plan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	name := opts.Name
	if name == "" {
		name = "kubesoc-" + now.UTC().Format("20060102-150405")
	}
	plan := &Plan{Name: name}
	namespaces := SOCNamespaces(opts.Tenants)
	labels := map[string]any{BackupSetLabel: name}

	if !opts.SkipVelero {
		obj, err := veleroBackup(ctx, opts, name, namespaces)
		if err != nil {
			return nil, err
		}
		obj.SetLabels(map[string]string{BackupSetLabel: name})
		plan.Objects = append(plan.Objects, PlannedObject{VeleroBackupGVR, obj})
	}

	for _, ns := range namespaces {
		planPostgreSQLBackups(ctx, opts, plan, ns, name, labels)
		planMariaDBBackups(ctx, opts, plan, ns, name, labels)
		planSnapshotJobs(ctx, opts, plan, ns, name, labels)
	}

	if len(plan.Objects) == 0 {
		plan.Notes = append(plan.Notes, "nothing to back up was found")
	}
	return plan, nil
}

// planPostgreSQLBackups adds a CloudNativePG Backup per Cluster that has
// backups configured, with the method and target of its ScheduledBackup.
func planPostgreSQLBackups(ctx context.Context, opts BackupOptions, plan *Plan,
	ns, name string, labels map[string]any,
) {
	clusters := listOrEmpty(ctx, opts.Dynamic, CNPGClusterGVR, ns)
	scheduled := listOrEmpty(ctx, opts.Dynamic, CNPGScheduledBackupGVR, ns)

	for _, c := range clusters {
		spec := map[string]any{"cluster": map[string]any{fieldName: c.GetName()}}
		for _, s := range scheduled {
			if cn, _, _ := unstructured.NestedString(s.Object, fieldSpec, "cluster", fieldName); cn != c.GetName() {
				continue
			}
			for _, k := range []string{"method", "target", "pluginConfiguration", "online"} {
				if v, ok, _ := unstructured.NestedFieldCopy(s.Object, fieldSpec, k); ok {
					spec[k] = v
				}
			}
		}
		_, hasBackup, _ := unstructured.NestedFieldNoCopy(c.Object, fieldSpec, "backup")
		if _, hasMethod := spec["method"]; !hasBackup && !hasMethod {
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"CNPG cluster %s/%s has no backup configured (spec.backup or a ScheduledBackup): skipped", ns, c.GetName()))
			continue
		}
		plan.Objects = append(plan.Objects, PlannedObject{CNPGBackupGVR, newObject(
			apiVersionCNPG, kindBackup, ns, name+"-"+c.GetName(), labels, spec)})
	}
}

// planMariaDBBackups adds a MariaDB Backup per MariaDB, copying the spec (and
// so the storage) of one of its scheduled Backups.
func planMariaDBBackups(ctx context.Context, opts BackupOptions, plan *Plan,
	ns, name string, labels map[string]any,
) {
	mariadbs := listOrEmpty(ctx, opts.Dynamic, MariaDBGVR, ns)
	backups := listOrEmpty(ctx, opts.Dynamic, MariaDBBackupGVR, ns)

	for _, db := range mariadbs {
		template := mariaDBBackupTemplate(backups, db.GetName())
		if template == nil {
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"MariaDB %s/%s has no Backup to copy the storage from: skipped", ns, db.GetName()))
			continue
		}
		delete(template, "schedule")
		plan.Objects = append(plan.Objects, PlannedObject{MariaDBBackupGVR, newObject(
			apiVersionMariaDB, kindBackup, ns, name+"-"+db.GetName(), labels, template)})
	}
}

// mariaDBBackupTemplate returns the spec of a Backup of the named MariaDB.
func mariaDBBackupTemplate(backups []unstructured.Unstructured, mariadb string) map[string]any {
	for _, b := range backups {
		if ref, _, _ := unstructured.NestedString(b.Object, fieldSpec, "mariaDbRef", fieldName); ref != mariadb {
			continue
		}
		if spec, ok, _ := unstructured.NestedMap(b.Object, fieldSpec); ok {
			return spec
		}
	}
	return nil
}

// planSnapshotJobs runs each OpenSearch snapshot CronJob once.
func planSnapshotJobs(ctx context.Context, opts BackupOptions, plan *Plan,
	ns, name string, labels map[string]any,
) {
	cronJobs, err := opts.Dynamic.Resource(CronJobGVR).Namespace(ns).List(ctx,
		metaV1.ListOptions{LabelSelector: SnapshotCronJobLabel})
	if err != nil {
		return
	}
	for _, cj := range cronJobs.Items {
		jobSpec, ok, _ := unstructured.NestedMap(cj.Object, fieldSpec, "jobTemplate", fieldSpec)
		if !ok {
			continue
		}
		plan.Objects = append(plan.Objects, PlannedObject{JobGVR, newObject(
			apiVersionBatch, kindJob, ns, truncateName(name+"-"+cj.GetName()), labels, jobSpec)})
	}
}

// veleroBackup builds the Velero Backup of the SOC namespaces, from the
// template of the named Schedule (or the one that covers the SOC namespace).
func veleroBackup(ctx context.Context, opts BackupOptions, name string, namespaces []string) (
	*unstructured.Unstructured, error,
) {
	var spec map[string]any
	for _, s := range listOrEmpty(ctx, opts.Dynamic, VeleroScheduleGVR, constants.NamespaceVelero) {
		included, _, _ := unstructured.NestedStringSlice(s.Object, fieldSpec, "template", "includedNamespaces")
		match := s.GetName() == opts.VeleroSchedule ||
			(opts.VeleroSchedule == "" && slices.Contains(included, constants.NamespaceSecurityOperations))
		if match {
			spec, _, _ = unstructured.NestedMap(s.Object, fieldSpec, "template")
			break
		}
	}
	if opts.VeleroSchedule != "" && spec == nil {
		return nil, fmt.Errorf("velero Schedule %q not found in namespace %s", opts.VeleroSchedule, constants.NamespaceVelero)
	}
	if spec == nil {
		// No Schedule to copy: a plain Backup kept for 30 days.
		spec = map[string]any{"ttl": "720h0m0s"}
	}
	ns := make([]any, 0, len(namespaces))
	for _, n := range namespaces {
		ns = append(ns, n)
	}
	spec["includedNamespaces"] = ns
	return newObject(apiVersionVelero, kindBackup, constants.NamespaceVelero, name, nil, spec), nil
}

// RestoreOptions are the inputs of PlanRestore.
type RestoreOptions struct {
	Dynamic dynamic.Interface
	// Backup is the backup set name (the Velero Backup's name).
	Backup string
	// Namespaces to restore; empty: every namespace the Velero Backup holds.
	Namespaces []string
	Now        time.Time
}

// PlanRestore lists the objects that restore a backup set: a Velero Restore
// (existing objects are left alone) and a MariaDB Restore per MariaDB Backup
// of the set. PostgreSQL (CNPG) cannot be restored in place; the plan's
// notes say how. It only reads.
func PlanRestore(ctx context.Context, opts RestoreOptions) (*Plan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	name := truncateName(opts.Backup + "-restore-" + now.UTC().Format("20060102-150405"))
	plan := &Plan{Name: name}

	backup, err := opts.Dynamic.Resource(VeleroBackupGVR).Namespace(constants.NamespaceVelero).
		Get(ctx, opts.Backup, metaV1.GetOptions{})
	if err == nil {
		plan.Objects = append(plan.Objects, PlannedObject{
			VeleroRestoreGVR,
			veleroRestore(name, backup.GetName(), opts.Namespaces),
		})
	} else {
		plan.Notes = append(plan.Notes, fmt.Sprintf("no Velero Backup %q: %v", opts.Backup, err))
	}

	namespaces := opts.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metaV1.NamespaceAll}
	}
	selector := BackupSetLabel + "=" + opts.Backup
	for _, ns := range namespaces {
		planMariaDBRestores(ctx, opts, plan, ns, name, selector)
		notePostgreSQLRestores(ctx, opts, plan, ns, selector)
	}
	// A restore of the indices is not automated: it closes or deletes the
	// live indices first, which is a decision for the operator, not a plan
	// that runs. The repository and prefix are the chart's
	// backup.opensearch.{repository,snapshotPrefix} (both "kubesoc").
	plan.Notes = append(plan.Notes,
		"OpenSearch indices: restore the tenant's snapshot through the indexer's _snapshot API"+
			" (POST _snapshot/<repository>/<snapshot>/_restore, after closing the live indices); not automated")
	return plan, nil
}

// veleroRestore builds the Velero Restore of a Backup. Objects that still
// exist are left alone (existingResourcePolicy none).
func veleroRestore(name, backup string, namespaces []string) *unstructured.Unstructured {
	spec := map[string]any{
		"backupName":             backup,
		"existingResourcePolicy": "none",
		"restorePVs":             true,
	}
	if len(namespaces) > 0 {
		ns := make([]any, 0, len(namespaces))
		for _, n := range namespaces {
			ns = append(ns, n)
		}
		spec["includedNamespaces"] = ns
	}
	return newObject(apiVersionVelero, kindRestore, constants.NamespaceVelero, name, nil, spec)
}

// planMariaDBRestores adds a MariaDB Restore per MariaDB Backup of the set.
func planMariaDBRestores(ctx context.Context, opts RestoreOptions, plan *Plan, ns, name, selector string) {
	backups, err := opts.Dynamic.Resource(MariaDBBackupGVR).Namespace(ns).
		List(ctx, metaV1.ListOptions{LabelSelector: selector})
	if err != nil {
		return
	}
	for _, b := range backups.Items {
		ref, _, _ := unstructured.NestedString(b.Object, fieldSpec, "mariaDbRef", fieldName)
		plan.Objects = append(plan.Objects, PlannedObject{MariaDBRestoreGVR, newObject(
			apiVersionMariaDB, kindRestore, b.GetNamespace(), truncateName(name+"-"+ref), nil,
			map[string]any{
				"mariaDbRef": map[string]any{fieldName: ref},
				"backupRef":  map[string]any{fieldName: b.GetName()},
			})})
	}
}

// notePostgreSQLRestores explains the CNPG recovery of each PostgreSQL Backup
// of the set: CloudNativePG restores into a new Cluster, never in place.
func notePostgreSQLRestores(ctx context.Context, opts RestoreOptions, plan *Plan, ns, selector string) {
	backups, err := opts.Dynamic.Resource(CNPGBackupGVR).Namespace(ns).
		List(ctx, metaV1.ListOptions{LabelSelector: selector})
	if err != nil {
		return
	}
	for _, b := range backups.Items {
		cluster, _, _ := unstructured.NestedString(b.Object, fieldSpec, "cluster", fieldName)
		plan.Notes = append(plan.Notes, fmt.Sprintf(
			"PostgreSQL %s/%s: CNPG restores into a new cluster only - set bootstrap.recovery.backup.name=%s "+
				"on a new Cluster (see the dfir-iris chart) and point IRIS at it", b.GetNamespace(), cluster, b.GetName()))
	}
}

// Execute creates the plan's objects, in order. It stops at the first error.
func (p *Plan) Execute(ctx context.Context, dyn dynamic.Interface) error {
	for _, o := range p.Objects {
		if _, err := dyn.Resource(o.GVR).Namespace(o.Object.GetNamespace()).
			Create(ctx, o.Object, metaV1.CreateOptions{}); err != nil {
			return fmt.Errorf("creating %s %s/%s: %w", o.Object.GetKind(), o.Object.GetNamespace(), o.Object.GetName(), err)
		}
	}
	return nil
}

// Print lists the plan.
func (p *Plan) Print(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%s:\n", p.Name)
	for _, o := range p.Objects {
		_, _ = fmt.Fprintf(w, "  %s %s/%s\n", o.Object.GetKind(), o.Object.GetNamespace(), o.Object.GetName())
	}
	for _, n := range p.Notes {
		_, _ = fmt.Fprintf(w, "  note: %s\n", n)
	}
}

// listOrEmpty lists a resource by name. A missing CRD (the operator is not
// installed) or any other listing error means nothing of that kind to back up,
// so it returns no items rather than an error.
func listOrEmpty(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, ns string,
) []unstructured.Unstructured {
	list, err := dyn.Resource(gvr).Namespace(ns).List(ctx, metaV1.ListOptions{})
	if err != nil {
		return nil
	}
	items := list.Items
	sort.Slice(items, func(i, j int) bool { return items[i].GetName() < items[j].GetName() })
	return items
}

func newObject(apiVersion, kind, ns, name string, labels map[string]any, spec map[string]any) *unstructured.Unstructured {
	meta := map[string]any{fieldName: truncateName(name), "namespace": ns}
	if labels != nil {
		meta["labels"] = labels
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   meta,
		"spec":       spec,
	}}
}

// truncateName keeps a name within the 63 character label limit.
func truncateName(s string) string {
	if len(s) <= 63 {
		return s
	}
	return strings.TrimRight(s[:63], "-")
}
