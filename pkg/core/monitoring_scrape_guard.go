// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"fmt"
	"sort"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

var (
	podMonitorGVR = schema.GroupVersionResource{
		Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors",
	}
	serviceMonitorGVR = schema.GroupVersionResource{
		Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors",
	}
)

// prometheusRBACRoleName is the Role/RoleBinding name kube-prometheus's jsonnet build renders
// into every namespace listed in prometheus_scrape_namespaces (see KubeAid's
// build/kube-prometheus/common-template.jsonnet). A namespace missing that Role is a namespace
// Prometheus can't list/watch pods or services in, regardless of whether a PodMonitor/
// ServiceMonitor exists there.
const prometheusRBACRoleName = "prometheus-k8s"

// CheckPrometheusScrapeNamespaces reports every namespace that has a PodMonitor or
// ServiceMonitor but no "prometheus-k8s" Role granting Prometheus access to it.
//
// prometheus_scrape_namespaces is a hand-maintained list in each cluster's *-vars.jsonnet, with
// nothing enforcing that it still covers every namespace an addon has since started dropping a
// PodMonitor/ServiceMonitor into. Left out, the gap is silent at deploy time and only surfaces
// later as a permanent PrometheusKubernetesListWatchFailures alert (Forbidden on list/watch) -
// this check catches it before that, against whatever the live cluster's RBAC actually grants.
func CheckPrometheusScrapeNamespaces(ctx context.Context) ([]string, error) {
	restConfig, err := kubernetes.CreateRESTConfig(ctx)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed creating dynamic client: %w", err)
	}

	clientset, err := kubernetes.CreateClientset(ctx)
	if err != nil {
		return nil, err
	}

	monitoredNamespaces := map[string]struct{}{}
	for _, gvr := range []schema.GroupVersionResource{podMonitorGVR, serviceMonitorGVR} {
		list, err := dynamicClient.Resource(gvr).Namespace(metaV1.NamespaceAll).
			List(ctx, metaV1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed listing %s: %w", gvr.Resource, err)
		}
		for _, item := range list.Items {
			monitoredNamespaces[item.GetNamespace()] = struct{}{}
		}
	}

	roles, err := clientset.RbacV1().Roles(metaV1.NamespaceAll).List(ctx, metaV1.ListOptions{
		FieldSelector: "metadata.name=" + prometheusRBACRoleName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed listing %s Roles: %w", prometheusRBACRoleName, err)
	}
	scrapableNamespaces := map[string]struct{}{}
	for _, role := range roles.Items {
		scrapableNamespaces[role.Namespace] = struct{}{}
	}

	missing := make([]string, 0, len(monitoredNamespaces))
	for namespace := range monitoredNamespaces {
		if _, ok := scrapableNamespaces[namespace]; !ok {
			missing = append(missing, namespace)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// MonitoringScrapeNamespacesCheck runs CheckPrometheusScrapeNamespaces against the current
// kubeconfig context and prints the result, for the "monitoring check-scrape-namespaces"
// command.
func MonitoringScrapeNamespacesCheck(ctx context.Context) {
	missing, err := CheckPrometheusScrapeNamespaces(ctx)
	assert.AssertErrNil(ctx, err, "Failed checking Prometheus scrape namespace RBAC")

	if len(missing) == 0 {
		fmt.Println( //nolint:forbidigo // operator-facing terminal output
			"OK: every namespace with a PodMonitor/ServiceMonitor has prometheus-k8s RBAC.",
		)
		return
	}

	fmt.Println( //nolint:forbidigo // operator-facing terminal output
		"Namespaces with a PodMonitor/ServiceMonitor but missing prometheus-k8s RBAC " +
			"(add them to prometheus_scrape_namespaces in this cluster's *-vars.jsonnet and " +
			"rebuild kube-prometheus):",
	)
	for _, namespace := range missing {
		fmt.Printf("  - %s\n", namespace) //nolint:forbidigo // operator-facing terminal output
	}
}
