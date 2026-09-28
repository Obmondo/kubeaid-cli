// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	k8sclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/siemctl"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// Flags shared by the lifecycle commands (clusterDir, generalConfig and
// sealedSecretsCert are declared with the render command).
var (
	flagYes    bool
	flagDryRun bool
)

const (
	flagNameYes    = "yes"
	flagNameDryRun = "dry-run"
)

// addClusterDirFlags adds --cluster-dir and --general-config.
func addClusterDirFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&clusterDir, flagNameClusterDir, "",
		"Local checkout of the cluster's directory (k8s/<cluster>) in the kubeaid-config repository")
	_ = cmd.MarkFlagDirname(flagNameClusterDir)
	_ = cmd.MarkFlagRequired(flagNameClusterDir)
	cmd.Flags().StringVar(&generalConfig, flagNameGeneralConfig, "",
		"General config with cluster.securityOperations. Default: "+generalConfigFileName+" in --"+flagNameClusterDir)
	_ = cmd.MarkFlagFilename(flagNameGeneralConfig)
}

// addSealingFlag adds --sealed-secrets-cert.
func addSealingFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&sealedSecretsCert, flagNameSealedSecretsCert, "",
		"Sealed-secrets controller certificate (file path or URL). Default: fetched from the cluster in $KUBECONFIG")
	_ = cmd.MarkFlagFilename(flagNameSealedSecretsCert)
}

func addYesFlag(cmd *cobra.Command, what string) {
	cmd.Flags().BoolVar(&flagYes, flagNameYes, false, "Do not ask for confirmation before "+what)
}

func addDryRunFlag(cmd *cobra.Command, what string) {
	cmd.Flags().BoolVar(&flagDryRun, flagNameDryRun, false, what)
}

// generalConfigPath resolves --general-config against --cluster-dir.
func generalConfigPath() string {
	if generalConfig != "" {
		return generalConfig
	}
	return filepath.Join(clusterDir, generalConfigFileName)
}

// requireClusterDir checks --cluster-dir is a directory.
func requireClusterDir() error {
	if info, err := os.Stat(clusterDir); err != nil || !info.IsDir() {
		return fmt.Errorf(
			"cluster directory %q does not exist - pass --%s with a local checkout of k8s/<cluster> in the kubeaid-config repository",
			clusterDir, flagNameClusterDir)
	}
	return nil
}

// loadConfig reads cluster.securityOperations (with defaults) from the
// general config and sets the sealing certificate source.
func loadConfig() (*config.SecurityOperationsConfig, error) {
	if err := requireClusterDir(); err != nil {
		return nil, err
	}
	if err := parser.LoadSecurityOperationsConfig(generalConfigPath()); err != nil {
		return nil, err
	}
	kubernetes.SealingCertSource = sealedSecretsCert
	return config.ParsedGeneralConfig.Cluster.SecurityOperations, nil
}

func tenantCodes(cfg *config.SecurityOperationsConfig) []string {
	codes := make([]string, 0, len(cfg.Tenants))
	for _, t := range cfg.Tenants {
		codes = append(codes, t.Code)
	}
	return codes
}

// clients are the Kubernetes clients of the cluster in $KUBECONFIG (or
// ~/.kube/config).
type clients struct {
	rest    *rest.Config
	kube    k8sclientset.Interface
	dynamic dynamic.Interface
}

func newClients(ctx context.Context) (*clients, error) {
	restCfg, err := kubernetes.CreateRESTConfig(ctx)
	if err != nil {
		return nil, err
	}
	kube, err := k8sclientset.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &clients{rest: restCfg, kube: kube, dynamic: dyn}, nil
}

// ReconcilerMetricsService is the Service the reconciler's long-running mode
// serves /status, /metrics and /healthz on (its port is named metrics).
const ReconcilerMetricsService = "siem-reconciler-metrics"

// reconcilerStatus GETs the reconciler's /status JSON through the API server's
// service proxy, so no port-forward is needed. An error (no long-running
// reconciler, no permission) makes the caller fall back to the last Job.
func (c *clients) reconcilerStatus(ctx context.Context) ([]byte, error) {
	return c.kube.CoreV1().Services(constants.NamespaceSecurityOperations).
		ProxyGet("http", ReconcilerMetricsService, "metrics", "/status", nil).DoRaw(ctx)
}

// podLogs returns the last lines of a pod's log.
func (c *clients) podLogs(ctx context.Context, namespace, pod string) (string, error) {
	tail := int64(20)
	raw, err := c.kube.CoreV1().Pods(namespace).GetLogs(pod, &coreV1.PodLogOptions{TailLines: &tail}).DoRaw(ctx)
	return string(raw), err
}

// connectedAgents runs `agent_control -l` in the tenant's Wazuh master
// (read-only) and counts the active agents.
func (c *clients) connectedAgents(ctx context.Context, namespace string) (string, error) {
	pod := namespace + "-manager-master-0"
	req := c.kube.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).
		SubResource("exec").VersionedParams(&coreV1.PodExecOptions{
		Command: []string{"/var/ossec/bin/agent_control", "-l"},
		Stdout:  true,
		Stderr:  true,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(c.rest, "POST", req.URL())
	if err != nil {
		return "", err
	}
	var out, stderr strings.Builder
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &out, Stderr: &stderr}); err != nil {
		return "", err
	}
	return fmt.Sprint(siemctl.CountActiveAgents(out.String())), nil
}

// confirm asks a yes/no question on the terminal; --yes answers it. Without a
// terminal and without --yes it refuses.
func confirm(cmd *cobra.Command, question string) error {
	if flagYes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("%s: refusing without a terminal; pass --%s", question, flagNameYes)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return errors.New("aborted")
}

// waitForEnter blocks until the operator presses ENTER.
func waitForEnter(cmd *cobra.Command, message string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("%s: needs a terminal", message)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s\nPress ENTER to continue, Ctrl+C to stop. ", message)
	_, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return err
}
