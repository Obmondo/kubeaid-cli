// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/kubectl/pkg/util/podutils"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// Status values reported for a backup resource by backup-exporter's GET /api/v1/backups.
// Mirrors backup-exporter's pkg/api — that module is not imported, kubeaid-cli only speaks its
// wire format.
const (
	backupStatusHealthy        = "healthy"
	backupStatusExceedsRPO     = "exceeds_rpo"
	backupStatusNoBackup       = "no_backup"
	backupStatusCollectorError = "collector_error"
	backupStatusUnknown        = "unknown"
)

// outputFormatJSON is the only accepted --output value.
const outputFormatJSON = "json"

// Resource-table column padding, matching kubectl's own printer
// (k8s.io/cli-runtime/pkg/printers.GetNewTabWriter).
const (
	tabwriterMinWidth = 6
	tabwriterTabWidth = 4
	tabwriterPadding  = 3
)

// backup-exporter's Service coordinates: the label its Helm chart stamps on the Service, the
// name its chart is expected to give the Service port that serves backupExporterAPIPath, and
// that HTTP path itself.
const (
	backupExporterLabelKey        = "app.kubernetes.io/name"
	backupExporterLabelValue      = "backup-exporter"
	backupExporterServicePortName = "http"
	backupExporterAPIPath         = "/api/v1/backups"
)

// portForwardReadyTimeout bounds how long fetchViaPortForward waits for the tunnel to come up
// before giving up - a dial that never resolves (e.g. a proxy silently dropping the upgrade)
// must not hang the CLI forever.
const portForwardReadyTimeout = 15 * time.Second

// backupExporterFetchTimeout bounds the HTTP GET issued over an established port-forward
// tunnel, independent of portForwardReadyTimeout, which only covers dialing.
const backupExporterFetchTimeout = 10 * time.Second

// backupCollector reports when an operator last finished a collection run. CollectedAt is nil
// when it has not completed a run yet, distinct from an operator that ran and found nothing.
// Every resource age below was measured at this timestamp, not at request time.
type backupCollector struct {
	Operator    string     `json:"operator"`
	CollectedAt *time.Time `json:"collected_at"`
}

// backupResource is one backup stream of one operator's backups for one resource. The age
// fields are nil when no series was published for them, distinct from a reported age of
// exactly zero (the operators' shared "no backup exists" sentinel).
type backupResource struct {
	Operator               string   `json:"operator"`
	Stream                 string   `json:"stream"`
	Namespace              string   `json:"namespace"`
	ResourceName           string   `json:"resource_name"`
	ResourceType           string   `json:"resource_type"`
	Method                 string   `json:"method,omitempty"`
	LatestBackupAgeSeconds *float64 `json:"latest_backup_age_seconds"`
	OldestBackupAgeSeconds *float64 `json:"oldest_backup_age_seconds"`
	MaxIntervalSeconds     *float64 `json:"max_interval_seconds"`
	Status                 string   `json:"status"`
	ErrorType              string   `json:"error_type,omitempty"`
}

// backupOperatorError is a collector failure with no resource identity (e.g. Velero failing to
// list its S3 bucket), so it can't be attached to a resource row.
type backupOperatorError struct {
	Operator string `json:"operator"`
	Type     string `json:"type"`
}

// backupResponse is the GET /api/v1/backups response body.
type backupResponse struct {
	Collectors     []backupCollector     `json:"collectors"`
	Resources      []backupResource      `json:"resources"`
	OperatorErrors []backupOperatorError `json:"operator_errors,omitempty"`
}

// BackupStatus fetches backup health from backup-exporter's JSON API and prints it.
// outputFormat "json" dumps the raw API response verbatim; any other value (including "")
// renders the human-readable report. Exits non-zero only when the fetch or decode itself
// fails - reported backup statuses (healthy/exceeds_rpo/no_backup/...) never affect the exit
// code.
func BackupStatus(ctx context.Context, outputFormat string) {
	// The current kubeconfig, exactly like kubectl: a status command must not
	// depend on the KubeOne artifact, whose endpoint is only reachable through
	// the provisioning SSH tunnel.
	clientset, err := kubernetes.CreateClientset(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing cluster client from your kubeconfig")

	// The port-forward dialer needs the raw *rest.Config (auth/TLS transport details) that
	// clientset itself doesn't expose.
	restConfig, err := kubernetes.CreateRESTConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing cluster client from your kubeconfig")

	body, err := fetchBackupStatus(ctx, clientset, restConfig)
	assert.AssertErrNil(ctx, err, "Failed fetching backup status from backup-exporter")

	if outputFormat == outputFormatJSON {
		_, err := os.Stdout.Write(body) //nolint:forbidigo // verbatim API passthrough, not a log line
		assert.AssertErrNil(ctx, err, "Failed writing backup status JSON to stdout")
		return
	}

	var response backupResponse
	err = json.Unmarshal(body, &response)
	assert.AssertErrNil(ctx, err, "Failed decoding backup-exporter response")

	fmt.Print(renderBackupStatus(response, time.Now())) //nolint:forbidigo // operator-facing terminal output
}

// findBackupExporterService locates backup-exporter's Service by label across every
// namespace. Errors when none or more than one match, naming the namespaces found so the
// operator knows what to look at. Returns the full Service (not just its namespace/name) so
// callers needing .Spec.Selector or .Spec.Ports - port-forwarding does - don't need a second
// List/Get round trip, and RBAC stays to `list` on services rather than also needing `get`.
func findBackupExporterService(ctx context.Context, clientset k8sclientset.Interface) (*coreV1.Service, error) {
	services, err := clientset.CoreV1().Services(metaV1.NamespaceAll).List(ctx, metaV1.ListOptions{
		LabelSelector: backupExporterLabelKey + "=" + backupExporterLabelValue,
	})
	if err != nil {
		return nil, fmt.Errorf("failed listing services labeled %s=%s: %w",
			backupExporterLabelKey, backupExporterLabelValue, err)
	}

	switch len(services.Items) {
	case 0:
		return nil, fmt.Errorf(
			"no backup-exporter service found (label %s=%s) in any namespace; is the backup-exporter chart installed?",
			backupExporterLabelKey, backupExporterLabelValue)

	case 1:
		svc := services.Items[0]
		return &svc, nil

	default:
		namespaces := make([]string, 0, len(services.Items))
		for _, svc := range services.Items {
			namespaces = append(namespaces, svc.Namespace)
		}
		sort.Strings(namespaces)
		return nil, fmt.Errorf(
			"found %d backup-exporter services (label %s=%s), expected exactly 1: namespaces %s",
			len(services.Items), backupExporterLabelKey, backupExporterLabelValue, strings.Join(namespaces, ", "))
	}
}

// fetchBackupStatus returns the raw GET /api/v1/backups response body, reached by port-forwarding
// to backup-exporter's pod - the pods/portforward subresource, the same mechanism `kubectl
// port-forward` uses. Unlike the services/proxy subresource this survives L7 API-server proxies
// (e.g. netbird's ClusterProxy) that 404 on services/proxy but pass through the upgraded
// SPDY/WebSocket stream pods/portforward negotiates.
func fetchBackupStatus(ctx context.Context, clientset k8sclientset.Interface, restConfig *restclient.Config) ([]byte, error) {
	svc, err := findBackupExporterService(ctx, clientset)
	if err != nil {
		return nil, err
	}

	pod, err := findReadyPod(ctx, clientset, svc.Namespace, svc.Name, svc.Spec.Selector)
	if err != nil {
		return nil, err
	}

	targetPort, err := resolveTargetPort(svc, pod)
	if err != nil {
		return nil, err
	}

	body, err := fetchViaPortForward(ctx, clientset, restConfig, svc.Namespace, pod.Name, targetPort)
	if err != nil {
		return nil, fmt.Errorf("failed fetching %s from backup-exporter pod %s/%s: %w",
			backupExporterAPIPath, svc.Namespace, pod.Name, err)
	}
	return body, nil
}

// findReadyPod lists pods in namespace matching selector - a Service's own .spec.selector, so
// this works regardless of a chart's label scheme rather than assuming backup-exporter's - and
// returns the first one that's Running with a Ready=True condition. Mirrors the readiness bar
// `kubectl port-forward service/...` applies when resolving a Service target to a pod.
func findReadyPod(ctx context.Context, clientset k8sclientset.Interface, namespace, serviceName string, selector map[string]string) (*coreV1.Pod, error) {
	if len(selector) == 0 {
		return nil, fmt.Errorf("service %s/%s has no pod selector", namespace, serviceName)
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metaV1.ListOptions{
		LabelSelector: labels.SelectorFromSet(selector).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed listing pods for service %s/%s: %w", namespace, serviceName, err)
	}

	for i := range pods.Items {
		if podReady(&pods.Items[i]) {
			return &pods.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no ready pod behind service %s/%s", namespace, serviceName)
}

// podReady reports whether pod is Running with a Ready=True condition - the same bar argo-cd's
// own port-forward pod selection uses (argo-cd/v2/util/kube.selectPodForPortForward, reached
// transitively via pkg/apiclient's PortForward option elsewhere in this codebase), and Ready
// alone isn't a substitute for: podutils.IsPodReady only inspects the condition, so a
// Succeeded/Failed pod that never cleared a stale Ready=True still needs the Phase check.
func podReady(pod *coreV1.Pod) bool {
	return pod.Status.Phase == coreV1.PodRunning && podutils.IsPodReady(pod)
}

// resolveTargetPort picks backup-exporter's Service port (named "http", or the Service's only
// port) and resolves it to a container port on pod: a numeric TargetPort is used directly, a
// named one is looked up by container port name, and an omitted TargetPort defaults to the
// Service's Port. Mirrors k8s.io/kubectl/pkg/util's target-port resolution semantics (that
// package's lookup helpers take value types tied to kubectl's own command options, so this
// reimplements rather than imports them).
func resolveTargetPort(svc *coreV1.Service, pod *coreV1.Pod) (int32, error) {
	svcPort, err := backupExporterServicePort(svc)
	if err != nil {
		return 0, err
	}

	switch {
	case svcPort.TargetPort.Type == intstr.String:
		return containerPortByName(pod, svcPort.TargetPort.StrVal)
	case svcPort.TargetPort.IntVal != 0:
		return svcPort.TargetPort.IntVal, nil
	default:
		// TargetPort omitted: Kubernetes defaults it to Port.
		return svcPort.Port, nil
	}
}

// backupExporterServicePort picks svc's port named backupExporterServicePortName, falling back
// to its only port when it declares just one (named or not) - the shape backup-exporter's chart
// is expected to use either way.
func backupExporterServicePort(svc *coreV1.Service) (coreV1.ServicePort, error) {
	if len(svc.Spec.Ports) == 1 {
		return svc.Spec.Ports[0], nil
	}
	for _, port := range svc.Spec.Ports {
		if port.Name == backupExporterServicePortName {
			return port, nil
		}
	}
	return coreV1.ServicePort{}, fmt.Errorf(
		"service %s/%s has %d ports and none named %q",
		svc.Namespace, svc.Name, len(svc.Spec.Ports), backupExporterServicePortName)
}

// containerPortByName looks up a named container port across pod's containers.
func containerPortByName(pod *coreV1.Pod, name string) (int32, error) {
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == name {
				return port.ContainerPort, nil
			}
		}
	}
	return 0, fmt.Errorf("pod %s/%s has no container port named %q", pod.Namespace, pod.Name, name)
}

// newPortForwardDialer builds the same dialer `kubectl port-forward` does (see
// k8s.io/kubectl/pkg/cmd/portforward's createDialer - unexported, so mirrored here rather than
// imported): a WebSocket-tunneled SPDY dialer tried first, falling back to plain SPDY when the
// upgrade itself fails or an HTTPS-terminating proxy gets in the way. The WebSocket path is
// what lets this survive L7 API-server proxies (e.g. netbird's ClusterProxy) that don't speak
// raw SPDY upgrades.
func newPortForwardDialer(clientset k8sclientset.Interface, restConfig *restclient.Config, namespace, podName string) (httpstream.Dialer, error) {
	portForwardURL := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward").
		URL()

	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed building SPDY round tripper: %w", err)
	}
	spdyDialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, portForwardURL)

	websocketDialer, err := portforward.NewSPDYOverWebsocketDialer(portForwardURL, restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed building WebSocket dialer: %w", err)
	}

	return portforward.NewFallbackDialer(websocketDialer, spdyDialer, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	}), nil
}

// fetchViaPortForward opens a port-forward to pod's targetPort, fetches backupExporterAPIPath
// over it, and tears the tunnel down before returning either way.
func fetchViaPortForward(ctx context.Context, clientset k8sclientset.Interface, restConfig *restclient.Config, namespace, podName string, targetPort int32) ([]byte, error) {
	dialer, err := newPortForwardDialer(clientset, restConfig, namespace, podName)
	if err != nil {
		return nil, fmt.Errorf("failed constructing port-forward dialer for pod %s/%s: %w", namespace, podName, err)
	}

	stopChan := make(chan struct{})
	readyChan := make(chan struct{})

	// Suppress the forwarder's own "Forwarding from ..." / "Handling connection ..." lines -
	// they'd otherwise land on stdout ahead of the table this command prints. Failures are
	// reported through the returned error instead.
	forwarder, err := portforward.New(dialer,
		[]string{fmt.Sprintf("0:%d", targetPort)},
		stopChan, readyChan,
		io.Discard, io.Discard,
	)
	if err != nil {
		return nil, fmt.Errorf("failed constructing port-forwarder for pod %s/%s: %w", namespace, podName, err)
	}

	forwardErrChan := make(chan error, 1)
	go func() {
		forwardErrChan <- forwarder.ForwardPorts()
	}()

	select {
	case <-readyChan:

	case err := <-forwardErrChan:
		return nil, fmt.Errorf("port-forward to pod %s/%s failed before becoming ready: %w", namespace, podName, err)

	case <-time.After(portForwardReadyTimeout):
		// dialer.Dial may still be blocked in-flight here - this is exactly the "proxy
		// silently drops the upgrade" failure this whole path exists to survive - and
		// forward()'s stopChan-select is only reached once Dial returns. Draining
		// forwardErrChan would block on that same hang and defeat the timeout, so don't:
		// close(stopChan) still stops it promptly if it's actually past dialing, and either
		// way the goroutine's buffered send never blocks even with nothing left to read it.
		close(stopChan)
		return nil, fmt.Errorf("timed out after %s waiting for port-forward to pod %s/%s to become ready",
			portForwardReadyTimeout, namespace, podName)

	case <-ctx.Done():
		// Same reasoning as the timeout case above: returning promptly on ctx cancellation
		// must not depend on a goroutine that may still be stuck inside an in-flight Dial.
		close(stopChan)
		return nil, fmt.Errorf("port-forward to pod %s/%s cancelled: %w", namespace, podName, ctx.Err())
	}

	ports, err := forwarder.GetPorts()
	if err != nil {
		close(stopChan)
		<-forwardErrChan
		return nil, fmt.Errorf("failed reading forwarded local port for pod %s/%s: %w", namespace, podName, err)
	}
	if len(ports) == 0 {
		close(stopChan)
		<-forwardErrChan
		return nil, fmt.Errorf("port-forward to pod %s/%s reported no forwarded ports", namespace, podName)
	}

	body, fetchErr := getBackupsOverLocalPort(ctx, ports[0].Local)

	close(stopChan)
	forwardErr := <-forwardErrChan

	if fetchErr != nil {
		return nil, fetchErr
	}
	// A stopChan-triggered shutdown returns nil from ForwardPorts. ErrLostConnectionToPod means
	// the remote side closed at roughly the same moment we asked it to - which happened after
	// getBackupsOverLocalPort already had its answer, so it isn't a real failure here.
	if forwardErr != nil && !errors.Is(forwardErr, portforward.ErrLostConnectionToPod) {
		return nil, fmt.Errorf("port-forward to pod %s/%s failed: %w", namespace, podName, forwardErr)
	}

	return body, nil
}

// getBackupsOverLocalPort fetches backupExporterAPIPath from the local end of an established
// port-forward tunnel, bounding the request with backupExporterFetchTimeout independent of the
// caller's own deadline.
func getBackupsOverLocalPort(ctx context.Context, localPort uint16) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, backupExporterFetchTimeout)
	defer cancel()

	fetchURL := fmt.Sprintf("http://127.0.0.1:%d%s", localPort, backupExporterAPIPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed building request for %s: %w", fetchURL, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed fetching %s: %w", fetchURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading response body from %s: %w", fetchURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, fetchURL, string(body))
	}

	return body, nil
}

// adjustedAge converts a backup age sampled at collectedAt into a duration as of now, per
// backup-exporter's contract: ages are measured when a collector runs, not when the API is
// called. Returns nil when there is no age to adjust (nil ageSeconds) or no reference point to
// adjust it from (nil collectedAt) - the latter shouldn't occur for a real resource, but
// showing nothing beats silently understating the true age.
func adjustedAge(ageSeconds *float64, collectedAt *time.Time, now time.Time) *time.Duration {
	if ageSeconds == nil || collectedAt == nil {
		return nil
	}
	adjusted := time.Duration(*ageSeconds*float64(time.Second)) + now.Sub(*collectedAt)
	return &adjusted
}

// formatLatestAge renders a resource's LATEST AGE cell: "-" when no series was published for
// it, "none" for the operators' shared no-backup sentinel (a reported age of exactly zero),
// otherwise the humanized, collector-adjusted age.
func formatLatestAge(r backupResource, collectedAt *time.Time, now time.Time) string {
	if r.LatestBackupAgeSeconds == nil {
		return "-"
	}
	if *r.LatestBackupAgeSeconds == 0 {
		return "none"
	}
	age := adjustedAge(r.LatestBackupAgeSeconds, collectedAt, now)
	if age == nil {
		return "-"
	}
	return duration.HumanDuration(*age)
}

// formatStatus renders a resource's STATUS cell, folding error_type into collector_error rows
// so the failure reason is visible without a dedicated column.
func formatStatus(r backupResource) string {
	if r.Status == backupStatusCollectorError && r.ErrorType != "" {
		return fmt.Sprintf("%s (%s)", r.Status, r.ErrorType)
	}
	return r.Status
}

// collectedAtByOperator indexes collectors by operator name, for per-resource age adjustment.
func collectedAtByOperator(collectors []backupCollector) map[string]*time.Time {
	result := make(map[string]*time.Time, len(collectors))
	for _, c := range collectors {
		result[c.Operator] = c.CollectedAt
	}
	return result
}

// collectorNeverCollected is the age stand-in for a collector that has not completed a run.
const collectorNeverCollected = "never"

// formatCollectorHeader condenses collector freshness into a single line: "collected 5m ago:
// cnpg | velero" when every collector reports the same age, otherwise per-operator ages. A
// collector that has never completed a run is named rather than dropped, so a silent operator
// cannot hide behind its peers' freshness.
func formatCollectorHeader(collectors []backupCollector, now time.Time) string {
	if len(collectors) == 0 {
		return "no collectors reported"
	}

	names := make([]string, 0, len(collectors))
	ages := make([]string, 0, len(collectors))
	shared := true

	for _, c := range collectors {
		age := collectorNeverCollected
		if c.CollectedAt != nil {
			age = duration.HumanDuration(now.Sub(*c.CollectedAt)) + " ago"
		}
		if len(ages) > 0 && age != ages[0] {
			shared = false
		}

		names = append(names, c.Operator)
		ages = append(ages, age)
	}

	if shared {
		if ages[0] == collectorNeverCollected {
			return "no completed collection run yet: " + strings.Join(names, " | ")
		}

		return fmt.Sprintf("collected %s: %s", ages[0], strings.Join(names, " | "))
	}

	parts := make([]string, 0, len(collectors))
	for i, name := range names {
		parts = append(parts, name+" "+ages[i])
	}

	return "collected: " + strings.Join(parts, " | ")
}

// statusOrder is the display order for the summary line: healthy first, then the statuses
// that need operator attention.
var statusOrder = []string{
	backupStatusHealthy,
	backupStatusExceedsRPO,
	backupStatusNoBackup,
	backupStatusCollectorError,
	backupStatusUnknown,
}

// countByStatus tallies resources per reported status.
func countByStatus(resources []backupResource) map[string]int {
	counts := make(map[string]int, len(resources))
	for _, r := range resources {
		counts[r.Status]++
	}
	return counts
}

// formatStatusSummary renders counts as "N healthy, M exceeds_rpo, ...", ordered by
// statusOrder with any status outside it (schema drift) sorted after - every count is shown,
// none dropped silently.
func formatStatusSummary(counts map[string]int) string {
	known := make(map[string]bool, len(statusOrder))
	parts := make([]string, 0, len(counts))
	for _, s := range statusOrder {
		known[s] = true
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}

	extra := make([]string, 0, len(counts))
	for s, n := range counts {
		if !known[s] && n > 0 {
			extra = append(extra, s)
		}
	}
	sort.Strings(extra)
	for _, s := range extra {
		parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
	}

	if len(parts) == 0 {
		return "no resources reported"
	}
	return strings.Join(parts, ", ")
}

// sortResources orders resources by namespace then resource name (the display contract),
// falling back to operator/stream/method so rows sharing a namespace+name (e.g. a CNPG
// cluster's logical and wal streams) render in a stable order across runs.
func sortResources(resources []backupResource) {
	sort.Slice(resources, func(i, j int) bool {
		a, b := resources[i], resources[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.ResourceName != b.ResourceName {
			return a.ResourceName < b.ResourceName
		}
		if a.Operator != b.Operator {
			return a.Operator < b.Operator
		}
		if a.Stream != b.Stream {
			return a.Stream < b.Stream
		}
		return a.Method < b.Method
	})
}

// anyMethod reports whether any resource carries a backup method, so the Velero-only METHOD
// column can be omitted entirely when every row would leave it blank.
func anyMethod(resources []backupResource) bool {
	for _, r := range resources {
		if r.Method != "" {
			return true
		}
	}
	return false
}

// renderResourceTable lays resources out as a plain aligned table, sorted per sortResources:
// an uppercase header row and one row per backup stream, the shape kubectl prints. The METHOD
// column appears only when at least one resource carries a method.
func renderResourceTable(resources []backupResource, collectedAt map[string]*time.Time, now time.Time) string {
	sorted := make([]backupResource, len(resources))
	copy(sorted, resources)
	sortResources(sorted)

	showMethod := anyMethod(sorted)

	headers := []string{"NAMESPACE", "RESOURCE", "TYPE", "STREAM"}
	if showMethod {
		headers = append(headers, "METHOD")
	}
	headers = append(headers, "LATEST AGE", "STATUS")

	var b strings.Builder

	w := tabwriter.NewWriter(&b, tabwriterMinWidth, tabwriterTabWidth, tabwriterPadding, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, r := range sorted {
		row := []string{r.Namespace, r.ResourceName, r.ResourceType, r.Stream}
		if showMethod {
			method := r.Method
			if method == "" {
				method = "-"
			}
			row = append(row, method)
		}
		row = append(row, formatLatestAge(r, collectedAt[r.Operator], now), formatStatus(r))

		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	// Flush only fails when the underlying writer does, and a strings.Builder never does.
	_ = w.Flush()

	return strings.TrimRight(b.String(), "\n")
}

// renderBackupStatus assembles the full human-readable report: a single collector freshness
// line, any operator errors, the resource table, then the status-count summary line.
// Returns a string rather than printing directly so the formatting itself stays testable.
func renderBackupStatus(resp backupResponse, now time.Time) string {
	var b strings.Builder

	fmt.Fprintln(&b, formatCollectorHeader(resp.Collectors, now))

	if len(resp.OperatorErrors) > 0 {
		fmt.Fprintln(&b, "\nOperator errors:")
		for _, e := range resp.OperatorErrors {
			fmt.Fprintf(&b, "  %s: %s\n", e.Operator, e.Type)
		}
	}

	// A headers-only table adds nothing beyond what "no resources reported" already says.
	if len(resp.Resources) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, renderResourceTable(resp.Resources, collectedAtByOperator(resp.Collectors), now))
	}

	fmt.Fprintln(&b, formatStatusSummary(countByStatus(resp.Resources)))

	return b.String()
}
