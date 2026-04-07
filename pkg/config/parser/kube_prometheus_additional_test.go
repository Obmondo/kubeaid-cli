// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

const (
	envVarKubePrometheusFailureCase = "KUBEPROMETHEUS_FAILURE_CASE"
	failureCaseIncompatibleVersion  = "incompatible-version"
	failureCaseUnknownK8sVersion    = "unknown-k8s-version"
	failureCaseMissingVPrefix       = "missing-v-prefix"
	failureCaseMalformedKPVersion   = "malformed-kp-version"
	failureCaseMalformedK8sVersion  = "malformed-k8s-version"
)

func TestValidateKubePrometheusVersion_ExitsOnIncompatibleVersion(t *testing.T) {
	if os.Getenv(envVarKubePrometheusFailureCase) == failureCaseIncompatibleVersion {
		ctx := context.Background()

		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cluster: config.ClusterConfig{
				K8sVersion: "v1.34.2",
			},
		}
		globals.CloudProviderName = constants.CloudProviderLocal

		// Should terminate with os.Exit(1).
		validateKubePrometheusVersion(ctx, "v0.18.0", "v1.34.2")
		t.Fatalf("expected validateKubePrometheusVersion to exit for incompatible version")
	}

	output, err := runSelfTestInSubprocess(
		t,
		"TestValidateKubePrometheusVersion_ExitsOnIncompatibleVersion",
		failureCaseIncompatibleVersion,
	)
	require.Error(t, err)
	assert.Contains(t, output, "aren't officially compatible")
}

func TestHydrateKubePrometheusVersion_ExitsOnUnknownK8sVersion(t *testing.T) {
	if os.Getenv(envVarKubePrometheusFailureCase) == failureCaseUnknownK8sVersion {
		ctx := context.Background()

		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cluster: config.ClusterConfig{
				K8sVersion: "v1.36.0",
			},
			KubePrometheus: &config.KubePrometheusConfig{},
		}

		// Should terminate with os.Exit(1).
		hydrateKubePrometheusVersion(ctx)
		t.Fatalf("expected hydrateKubePrometheusVersion to exit for unknown k8s version")
	}

	output, err := runSelfTestInSubprocess(
		t,
		"TestHydrateKubePrometheusVersion_ExitsOnUnknownK8sVersion",
		failureCaseUnknownK8sVersion,
	)
	require.Error(t, err)
	assert.Contains(t, output, "Unsupported Kubernetes version")
}

func TestValidateKubePrometheusVersion_ExitsOnMissingVPrefix(t *testing.T) {
	if os.Getenv(envVarKubePrometheusFailureCase) == failureCaseMissingVPrefix {
		ctx := context.Background()

		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cluster: config.ClusterConfig{
				K8sVersion: "v1.34.0",
			},
		}
		globals.CloudProviderName = constants.CloudProviderLocal

		// Should terminate with os.Exit(1) — missing 'v' prefix.
		validateKubePrometheusVersion(ctx, "0.16.0", "v1.34.0")
		t.Fatalf("expected validateKubePrometheusVersion to exit for missing 'v' prefix")
	}

	output, err := runSelfTestInSubprocess(
		t,
		"TestValidateKubePrometheusVersion_ExitsOnMissingVPrefix",
		failureCaseMissingVPrefix,
	)
	require.Error(t, err)
	assert.Contains(t, output, "KubePrometheus version must start with 'v'")
}

func TestValidateKubePrometheusVersion_ExitsOnMalformedVersion(t *testing.T) {
	if os.Getenv(envVarKubePrometheusFailureCase) == failureCaseMalformedKPVersion {
		ctx := context.Background()

		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cluster: config.ClusterConfig{
				K8sVersion: "v1.34.0",
			},
		}
		globals.CloudProviderName = constants.CloudProviderLocal

		// Should terminate with os.Exit(1) — malformed version (has 'v' but invalid format).
		validateKubePrometheusVersion(ctx, "vinvalid", "v1.34.0")
		t.Fatalf("expected validateKubePrometheusVersion to exit for malformed version")
	}

	output, err := runSelfTestInSubprocess(
		t,
		"TestValidateKubePrometheusVersion_ExitsOnMalformedVersion",
		failureCaseMalformedKPVersion,
	)
	require.Error(t, err)
	assert.Contains(t, output, "Failed parsing KubePrometheus semantic version")
}

func TestHydrateKubePrometheusVersion_ExitsOnMalformedK8sVersion(t *testing.T) {
	if os.Getenv(envVarKubePrometheusFailureCase) == failureCaseMalformedK8sVersion {
		ctx := context.Background()

		config.ParsedGeneralConfig = &config.GeneralConfig{
			Cluster: config.ClusterConfig{
				K8sVersion: "not-a-version",
			},
			KubePrometheus: &config.KubePrometheusConfig{},
		}

		// Should terminate with os.Exit(1) — malformed K8s version.
		hydrateKubePrometheusVersion(ctx)
		t.Fatalf("expected hydrateKubePrometheusVersion to exit for malformed K8s version")
	}

	output, err := runSelfTestInSubprocess(
		t,
		"TestHydrateKubePrometheusVersion_ExitsOnMalformedK8sVersion",
		failureCaseMalformedK8sVersion,
	)
	require.Error(t, err)
	assert.Contains(t, output, "Failed parsing Kubernetes semantic version")
}

func runSelfTestInSubprocess(t *testing.T, testName, failureCase string) (string, error) {
	t.Helper()

	var cmd *exec.Cmd
	switch testName {
	case "TestValidateKubePrometheusVersion_ExitsOnIncompatibleVersion":
		//nolint:gosec // Intentional subprocess execution of the current test binary.
		cmd = exec.Command(
			os.Args[0],
			"-test.run",
			"^TestValidateKubePrometheusVersion_ExitsOnIncompatibleVersion$",
		)
	case "TestHydrateKubePrometheusVersion_ExitsOnUnknownK8sVersion":
		//nolint:gosec // Intentional subprocess execution of the current test binary.
		cmd = exec.Command(
			os.Args[0],
			"-test.run",
			"^TestHydrateKubePrometheusVersion_ExitsOnUnknownK8sVersion$",
		)
	case "TestValidateKubePrometheusVersion_ExitsOnMissingVPrefix":
		//nolint:gosec // Intentional subprocess execution of the current test binary.
		cmd = exec.Command(
			os.Args[0],
			"-test.run",
			"^TestValidateKubePrometheusVersion_ExitsOnMissingVPrefix$",
		)
	case "TestValidateKubePrometheusVersion_ExitsOnMalformedVersion":
		//nolint:gosec // Intentional subprocess execution of the current test binary.
		cmd = exec.Command(
			os.Args[0],
			"-test.run",
			"^TestValidateKubePrometheusVersion_ExitsOnMalformedVersion$",
		)
	case "TestHydrateKubePrometheusVersion_ExitsOnMalformedK8sVersion":
		//nolint:gosec // Intentional subprocess execution of the current test binary.
		cmd = exec.Command(
			os.Args[0],
			"-test.run",
			"^TestHydrateKubePrometheusVersion_ExitsOnMalformedK8sVersion$",
		)
	default:
		t.Fatalf("unexpected test name: %s", testName)
	}

	cmd.Env = append(os.Environ(), envVarKubePrometheusFailureCase+"="+failureCase)
	output, err := cmd.CombinedOutput()

	return string(output), err
}
