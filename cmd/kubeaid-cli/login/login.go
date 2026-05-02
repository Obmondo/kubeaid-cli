// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

// Package login implements the `kubeaid-cli login` subcommand.
//
// login is intentionally self-contained: it does not parse general.yaml or
// secrets.yaml and does not proxy to the kubeaid-core container. By default
// it queries the local NetBird daemon for clusters the user can currently
// reach, intersects that with the local klist clone, and shows an
// interactive picker. With --cert (or KUBEAID_CERT), it falls back to a
// non-interactive cert-driven flow that derives the cluster identity from
// the puppet cert's Subject CN. After writing the kubeconfig it invokes
// kubelogin to warm the token cache; --no-authenticate skips that step.
package login

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/klist"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/netbird"
)

const (
	flagCert           = "cert"
	flagRegistry       = "registry"
	flagKubeconfig     = "kubeconfig"
	flagNoAuthenticate = "no-authenticate"

	envCert       = "KUBEAID_CERT"
	envRegistry   = "KUBEAID_REGISTRY"
	envKubeconfig = "KUBECONFIG"

	// defaultCert is intentionally empty — absence of --cert/KUBEAID_CERT
	// puts login in interactive mode, where the user picks from clusters
	// the local NetBird daemon reports as accessible.
	defaultCert       = ""
	defaultRegistry   = "~/.config/klist"
	defaultKubeconfig = "~/.kube/config"

	kubeloginBinary = "kubelogin"
	kubeloginRepo   = "https://github.com/int128/kubelogin"
)

var flags struct {
	cert           string
	registry       string
	kubeconfig     string
	noAuthenticate bool
}

// LoginCmd is the cobra command for `kubeaid-cli login`.
var LoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Pick a cluster you can reach over NetBird and write its kubeconfig",
	Long: `login asks the local NetBird daemon which clusters you can currently
reach, intersects that with the clusters in your local klist clone, and
shows an interactive picker. The chosen cluster's YAML is rendered into
a kubeconfig that uses kubelogin for OIDC authentication; kubelogin is
then invoked to warm the token cache (skip with --no-authenticate).

For non-interactive use (CI, scripts), pass --cert: login then reads the
cluster identity from the puppet cert's Subject CN and skips the picker.

No Docker, no general.yaml, no secrets.yaml needed.`,

	RunE: runLogin,
}

func init() {
	// RunE-returned errors are real runtime failures (kubelogin can't
	// reach Keycloak, picker aborted, etc.) — printing the full flag
	// usage after them just adds noise. Cobra still prints the usage
	// block for genuine flag errors (unknown flag, parse error, etc.).
	LoginCmd.SilenceUsage = true

	// kubeaid-cli main.go logs RunE errors via slog.Error already, so
	// suppressing cobra's own "Error: ..." print avoids the duplicate
	// line (cobra would print it once, slog would print it again).
	LoginCmd.SilenceErrors = true

	LoginCmd.Flags().StringVar(&flags.cert, flagCert, "",
		fmt.Sprintf("path to puppet cert PEM for non-interactive mode "+
			"(env: %s; if unset, login is interactive and discovers "+
			"clusters from the NetBird daemon)", envCert))
	LoginCmd.Flags().StringVar(&flags.registry, flagRegistry, "",
		fmt.Sprintf("path to local klist clone (env: %s, default: %s)", envRegistry, defaultRegistry))
	LoginCmd.Flags().StringVar(&flags.kubeconfig, flagKubeconfig, "",
		fmt.Sprintf("kubeconfig output path (env: %s, default: %s)", envKubeconfig, defaultKubeconfig))
	LoginCmd.Flags().BoolVar(&flags.noAuthenticate, flagNoAuthenticate, false,
		"skip the kubelogin OIDC step and only write the kubeconfig")
}

func runLogin(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	certPath := resolveInput(flags.cert, envCert, defaultCert)
	registryPath := resolveInput(flags.registry, envRegistry, defaultRegistry)
	kubeconfigPath := resolveInput(flags.kubeconfig, envKubeconfig, defaultKubeconfig)

	certPath = expandTilde(certPath)
	registryPath = expandTilde(registryPath)
	kubeconfigPath = expandTilde(kubeconfigPath)

	clusterName, customerID, err := identifyCluster(ctx, certPath, registryPath)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "derived cluster identity",
		slog.String("clusterName", clusterName),
		slog.String("customerID", customerID),
	)

	cfg, err := klist.Load(registryPath, clusterName, customerID)
	if err != nil {
		return fmt.Errorf("loading cluster config from registry: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	kubeconfigBytes, err := buildKubeconfig(cfg, clusterName, customerID)
	if err != nil {
		return fmt.Errorf("building kubeconfig: %w", err)
	}

	if err := writeKubeconfig(kubeconfigPath, kubeconfigBytes); err != nil {
		return fmt.Errorf("writing kubeconfig: %w", err)
	}

	fmt.Printf("kubeconfig written to %s (cluster: %s.%s)\n",
		kubeconfigPath, clusterName, customerID)

	if flags.noAuthenticate {
		return nil
	}

	kubeloginPath, err := lookupKubelogin()
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "starting OIDC login via kubelogin",
		slog.String("path", kubeloginPath),
	)

	if err := runKubelogin(ctx, kubeloginPath, kubeloginArgs(cfg)); err != nil {
		// kubelogin printed its verbose error to stderr already (we
		// teed it through). The slog ERROR line below this prose will
		// be the categorised message classifyKubeloginErr produced.
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "The kubeconfig is on disk, so you can:")
		fmt.Fprintf(os.Stderr,
			"  • run `kubectl <cmd>` — kubectl will retry kubelogin\n"+
				"  • rerun `kubeaid-cli login --%s` to skip OIDC entirely\n\n",
			flagNoAuthenticate)

		return err
	}

	fmt.Println("authenticated; token cached")

	return nil
}

// kubeloginArgs returns the argv that drives kubelogin. The same list is
// embedded in the kubeconfig exec block (so kubectl invokes kubelogin with
// the same flags later) and used by runLogin to warm the token cache up
// front, so the two paths cannot drift.
func kubeloginArgs(cfg *klist.ClusterConfig) []string {
	return []string{
		"get-token",
		"--oidc-issuer-url=" + cfg.OIDC.IssuerURL,
		"--oidc-client-id=" + cfg.OIDC.ClientID,
		"--oidc-extra-scope=email",
		"--oidc-extra-scope=groups",
	}
}

// lookupKubelogin verifies that the kubelogin binary is available on PATH.
// It returns the resolved path and a wrapped error with install guidance on
// miss. Defined as a variable so tests can stub it.
var lookupKubelogin = func() (string, error) {
	path, err := exec.LookPath(kubeloginBinary)
	if err != nil {
		return "", fmt.Errorf(
			"%s not found on PATH: %w (install from %s, or rerun with --%s to skip)",
			kubeloginBinary, err, kubeloginRepo, flagNoAuthenticate,
		)
	}

	return path, nil
}

// runKubelogin executes the kubelogin binary so it performs the OIDC
// PKCE flow and writes its token to the on-disk cache. stderr is teed
// to the user (so kubelogin's "Open the following URL" prompt and any
// error are visible) and to a buffer; on failure classifyKubeloginErr
// inspects the buffer to surface a categorised slog ERROR alongside
// kubelogin's own message. stdout (the ExecCredential JSON) is
// discarded — for kubectl only. Defined as a variable so tests can
// stub it.
var runKubelogin = func(ctx context.Context, path string, args []string) error {
	var stderrCapture bytes.Buffer

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrCapture)
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		return classifyKubeloginErr(err, stderrCapture.String())
	}

	return nil
}

// classifyKubeloginErr converts kubelogin's verbose multi-layer stderr
// ("error: get-token: authentication error: oidc error: oidc discovery
// error: Get \"…\": dial tcp: lookup …") into a single-line, action-
// oriented error suitable for the slog ERROR line. The full kubelogin
// output is still visible to the user inline (we tee it through), so
// the wrapping error is just a categorisation hint.
func classifyKubeloginErr(err error, stderrText string) error {
	if strings.Contains(stderrText, "oidc discovery error") {
		switch {
		case strings.Contains(stderrText, "no such host"):
			return fmt.Errorf(
				"OIDC discovery: issuer hostname is not resolvable — check oidc.issuerUrl in the cluster's klist YAML")

		case strings.Contains(stderrText, "server misbehaving"):
			return fmt.Errorf(
				"OIDC discovery: DNS lookup failed (server misbehaving) — check your DNS / NetBird mesh")

		case strings.Contains(stderrText, "connection refused"):
			return fmt.Errorf(
				"OIDC discovery: issuer is not listening on that address — Keycloak down, or wrong port?")

		case strings.Contains(stderrText, "i/o timeout"),
			strings.Contains(stderrText, "context deadline exceeded"):
			return fmt.Errorf(
				"OIDC discovery: issuer reachable but did not respond in time — network or NetBird path slow?")

		case strings.Contains(stderrText, "x509"):
			return fmt.Errorf(
				"OIDC discovery: TLS error reaching issuer — check Keycloak's certificate / system trust store")

		default:
			return fmt.Errorf(
				"OIDC discovery: failed to reach issuer (see kubelogin output above)")
		}
	}

	if strings.Contains(stderrText, "context canceled") {
		return fmt.Errorf("kubelogin: cancelled before authentication completed")
	}

	// Uncategorised — include the first non-empty stderr line so the
	// ERROR slog line still has a clue (we suppressed kubelogin's
	// verbose chain to stderr, so without this the user would only see
	// "exit status 1").
	if first := firstNonEmptyLine(stderrText); first != "" {
		return fmt.Errorf("kubelogin: %s", first)
	}

	return fmt.Errorf("kubelogin authentication failed: %w", err)
}

// firstNonEmptyLine returns the first non-blank line of s, trimmed of
// leading/trailing whitespace. Returns "" if no such line exists.
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}

	return ""
}

// identifyCluster decides which cluster the user wants. With certPath
// set, it reads the puppet cert (non-interactive — useful for CI). With
// certPath empty, it queries the local NetBird daemon for accessible
// peers and shows an interactive picker filtered against the klist
// registry.
func identifyCluster(ctx context.Context, certPath, registryPath string) (string, string, error) {
	if certPath != "" {
		slog.InfoContext(ctx, "reading cert", slog.String("path", certPath))

		cn, err := cert.ReadCN(certPath)
		if err != nil {
			return "", "", fmt.Errorf("reading cert CN: %w", err)
		}

		return cert.SplitCN(cn)
	}

	return pickCluster(ctx, registryPath)
}

// pickCluster runs the interactive flow: read klist's global.yaml, ask
// the local NetBird daemon which clusters are reachable right now, walk
// the klist registry, intersect, and prompt the user to choose.
//
// Returns the selected (clusterName, customerID).
var pickCluster = func(ctx context.Context, registryPath string) (string, string, error) {
	global, err := klist.LoadGlobal(registryPath)
	if err != nil {
		return "", "", fmt.Errorf("loading klist global config: %w", err)
	}

	status, err := netbird.FetchStatus(ctx)
	if err != nil {
		return "", "", fmt.Errorf("querying netbird daemon: %w "+
			"(is netbird installed and is the daemon running?)", err)
	}

	if status.DaemonStatus != netbird.DaemonStatusConnected {
		return "", "", fmt.Errorf(
			"netbird daemon is %q, not %q — run `netbird up` first",
			status.DaemonStatus, netbird.DaemonStatusConnected,
		)
	}

	if global.NetBird.ManagementURL != "" &&
		!strings.EqualFold(global.NetBird.ManagementURL, status.Management.URL) {
		slog.WarnContext(ctx, "netbird management URL differs from klist global.yaml",
			slog.String("daemon", status.Management.URL),
			slog.String("expected", global.NetBird.ManagementURL),
		)
	}

	accessible := netbird.AccessibleClusters(status,
		global.NetBird.Prefix(), global.NetBird.Suffix())

	refs, err := klist.ListClusters(registryPath)
	if err != nil {
		return "", "", fmt.Errorf("listing klist clusters: %w", err)
	}

	candidates := intersectClusters(refs, accessible)
	if len(candidates) == 0 {
		return "", "", fmt.Errorf(
			"no clusters are both reachable on NetBird and present in the klist " +
				"registry — check your NetBird group memberships and that the klist " +
				"clone is up to date")
	}

	byCustomer := groupByCustomer(candidates)

	customerID, err := pickCustomer(byCustomer)
	if err != nil {
		return "", "", err
	}

	clusterName, err := pickClusterWithin(customerID, byCustomer[customerID])
	if err != nil {
		return "", "", err
	}

	return clusterName, customerID, nil
}

// groupByCustomer buckets ClusterRefs by Customer. Returned map preserves
// the input order within each customer (ListClusters already sorts the
// flat list by (customer, cluster)).
func groupByCustomer(refs []klist.ClusterRef) map[string][]klist.ClusterRef {
	out := make(map[string][]klist.ClusterRef)
	for _, r := range refs {
		out[r.Customer] = append(out[r.Customer], r)
	}

	return out
}

// pickCustomer prompts the user to choose a customer when more than one
// is available. With a single customer it auto-selects.
var pickCustomer = func(byCustomer map[string][]klist.ClusterRef) (string, error) {
	if len(byCustomer) == 1 {
		for c := range byCustomer {
			return c, nil
		}
	}

	customers := sortedKeys(byCustomer)

	options := make([]huh.Option[string], 0, len(customers))

	for _, c := range customers {
		count := len(byCustomer[c])

		label := fmt.Sprintf("%s (%d cluster%s)", c, count, plural(count))
		options = append(options, huh.NewOption(label, c))
	}

	var picked string
	if err := huh.NewSelect[string]().
		Title("Pick a customer").
		Description(fmt.Sprintf("%d customer(s) reachable via NetBird", len(byCustomer))).
		Options(options...).
		Value(&picked).
		Run(); err != nil {
		return "", fmt.Errorf("customer picker: %w", err)
	}

	return picked, nil
}

// pickClusterWithin prompts for a cluster inside the chosen customer.
// With a single cluster it auto-selects.
var pickClusterWithin = func(customer string, refs []klist.ClusterRef) (string, error) {
	if len(refs) == 1 {
		return refs[0].ClusterName, nil
	}

	options := make([]huh.Option[string], 0, len(refs))

	for _, r := range refs {
		options = append(options, huh.NewOption(r.ClusterName, r.ClusterName))
	}

	var picked string
	if err := huh.NewSelect[string]().
		Title(fmt.Sprintf("Pick a cluster in %s", customer)).
		Description(fmt.Sprintf("%d cluster(s) in %s", len(refs), customer)).
		Options(options...).
		Value(&picked).
		Run(); err != nil {
		return "", fmt.Errorf("cluster picker: %w", err)
	}

	return picked, nil
}

func sortedKeys(m map[string][]klist.ClusterRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}

	return "s"
}

// intersectClusters returns the klist refs whose ClusterName appears in
// accessible. accessible can contain duplicates (e.g. two customers
// expose a cluster named "staging"); both klist refs are kept.
func intersectClusters(refs []klist.ClusterRef, accessible []string) []klist.ClusterRef {
	allowed := make(map[string]struct{}, len(accessible))
	for _, name := range accessible {
		allowed[name] = struct{}{}
	}

	out := make([]klist.ClusterRef, 0, len(refs))

	for _, r := range refs {
		if _, ok := allowed[r.ClusterName]; ok {
			out = append(out, r)
		}
	}

	return out
}

// resolveInput returns the first non-empty value among: explicit flag,
// environment variable, then the built-in default.
func resolveInput(flagVal, envKey, defaultVal string) string {
	if flagVal != "" {
		return flagVal
	}

	if v := os.Getenv(envKey); v != "" {
		return v
	}

	return defaultVal
}

// expandTilde replaces a leading "~/" with the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to literal path; the subsequent file operation will surface
		// the real error.
		return path
	}

	return filepath.Join(home, path[2:])
}

// kubeconfig is a minimal YAML representation of a kubeconfig. Using a local
// struct instead of k8s.io/client-go/tools/clientcmd/api avoids pulling in
// that package's json tag assumptions and allows us to emit exactly the shape
// defined in the design doc.
type kubeconfig struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Clusters       []namedCluster `yaml:"clusters"`
	Contexts       []namedContext `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
	Users          []namedUser    `yaml:"users"`
}

type namedCluster struct {
	Name    string      `yaml:"name"`
	Cluster clusterInfo `yaml:"cluster"`
}

type clusterInfo struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
}

type namedContext struct {
	Name    string      `yaml:"name"`
	Context contextInfo `yaml:"context"`
}

type contextInfo struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type namedUser struct {
	Name string   `yaml:"name"`
	User userInfo `yaml:"user"`
}

type userInfo struct {
	Exec execConfig `yaml:"exec"`
}

type execConfig struct {
	APIVersion string   `yaml:"apiVersion"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args"`
}

func buildKubeconfig(cfg *klist.ClusterConfig, clusterName, customerID string) ([]byte, error) {
	contextName := clusterName + "." + customerID

	caData := base64.StdEncoding.EncodeToString([]byte(cfg.CABundle))

	kc := kubeconfig{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []namedCluster{
			{
				Name: contextName,
				Cluster: clusterInfo{
					Server:                   cfg.Server,
					CertificateAuthorityData: caData,
				},
			},
		},
		Contexts: []namedContext{
			{
				Name: contextName,
				Context: contextInfo{
					Cluster: contextName,
					User:    "oidc",
				},
			},
		},
		CurrentContext: contextName,
		Users: []namedUser{
			{
				Name: "oidc",
				User: userInfo{
					Exec: execConfig{
						APIVersion: "client.authentication.k8s.io/v1beta1",
						Command:    kubeloginBinary,
						Args:       kubeloginArgs(cfg),
					},
				},
			},
		},
	}

	return yaml.Marshal(kc)
}

// writeKubeconfig creates intermediate directories and atomically writes
// content to path with mode 0600.
func writeKubeconfig(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating parent directories for %q: %w", path, err)
	}

	// Write to a temp file in the same directory first so that the final
	// rename is atomic on POSIX systems.
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".kubeconfig-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %q: %w", dir, err)
	}

	tmpName := tmp.Name()

	_, writeErr := tmp.Write(content)
	closeErr := tmp.Close()

	if writeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("writing kubeconfig temp file: %w", writeErr)
	}

	if closeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing kubeconfig temp file: %w", closeErr)
	}

	if err := os.Chmod(tmpName, 0o600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("setting permissions on kubeconfig temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file to %q: %w", path, err)
	}

	return nil
}
