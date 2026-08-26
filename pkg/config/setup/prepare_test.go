// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/globals"
)

// Mutates globals.LogFilePath; not parallel.
func TestRelocateLogFile(t *testing.T) {
	origLogFilePath := globals.LogFilePath
	t.Cleanup(func() { globals.LogFilePath = origLogFilePath })

	t.Run("moves the file and repoints globals", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "shared-logs", "run.log")
		if err := os.MkdirAll(filepath.Dir(oldPath), 0o750); err != nil {
			t.Fatal(err)
		}

		// Held open across the relocation — the contract is that the
		// descriptor survives the rename.
		logFile, err := os.OpenFile(oldPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { logFile.Close() })
		if _, err := logFile.WriteString("line one\n"); err != nil {
			t.Fatal(err)
		}
		globals.LogFilePath = oldPath

		clusterLogs := filepath.Join(root, "prod", "logs")
		RelocateLogFile(t.Context(), clusterLogs)

		wantPath := filepath.Join(clusterLogs, "run.log")
		if globals.LogFilePath != wantPath {
			t.Fatalf("LogFilePath = %q, want %q", globals.LogFilePath, wantPath)
		}
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Fatalf("old log file still present at %q", oldPath)
		}

		if _, err := logFile.WriteString("line two\n"); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "line one\nline two\n" {
			t.Fatalf("moved log content = %q — the open descriptor did not survive the rename", content)
		}
	})

	t.Run("no-op without a log file", func(t *testing.T) {
		globals.LogFilePath = ""
		RelocateLogFile(t.Context(), filepath.Join(t.TempDir(), "logs"))
		if globals.LogFilePath != "" {
			t.Fatalf("LogFilePath = %q, want empty", globals.LogFilePath)
		}
	})

	t.Run("no-op when the file is already in place", func(t *testing.T) {
		logs := filepath.Join(t.TempDir(), "logs")
		if err := os.MkdirAll(logs, 0o750); err != nil {
			t.Fatal(err)
		}
		inPlace := filepath.Join(logs, "run.log")
		if err := os.WriteFile(inPlace, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		globals.LogFilePath = inPlace

		RelocateLogFile(t.Context(), logs)

		if globals.LogFilePath != inPlace {
			t.Fatalf("LogFilePath = %q, want unchanged %q", globals.LogFilePath, inPlace)
		}
	})

	t.Run("keeps the old path when the move fails", func(t *testing.T) {
		root := t.TempDir()
		oldPath := filepath.Join(root, "missing", "run.log")
		globals.LogFilePath = oldPath

		RelocateLogFile(t.Context(), filepath.Join(root, "cluster", "logs"))

		if globals.LogFilePath != oldPath {
			t.Fatalf("LogFilePath = %q, want unchanged %q", globals.LogFilePath, oldPath)
		}
	})
}

// Mutates the constants output-path vars; not parallel.
func TestUseOutputsHome(t *testing.T) {
	orig := []*string{
		&constants.OutputsDirectory,
		&constants.OutputLogsDirectory,
		&constants.OutputPathManagementClusterK3DConfig,
		&constants.OutputPathManagementClusterHostKubeconfig,
		&constants.OutputPathManagementClusterContainerKubeconfig,
		&constants.OutputPathMainClusterKubeconfig,
		&constants.OutputPathJWKSDocument,
	}
	origValues := make([]string, len(orig))
	for i, p := range orig {
		origValues[i] = *p
	}
	t.Cleanup(func() {
		for i, p := range orig {
			*p = origValues[i]
		}
	})

	home := "/home/operator/.config/kubeaid-cli/prod"
	constants.UseOutputsHome(home)

	tests := []struct {
		name string
		got  *string
		want string
	}{
		{"OutputsDirectory", &constants.OutputsDirectory, home},
		{"OutputLogsDirectory", &constants.OutputLogsDirectory, home + "/logs"},
		{"OutputPathManagementClusterK3DConfig", &constants.OutputPathManagementClusterK3DConfig, home + "/k3d.config.yaml"},
		{"OutputPathManagementClusterHostKubeconfig", &constants.OutputPathManagementClusterHostKubeconfig, home + "/kubeconfigs/management/host.yaml"},
		{"OutputPathManagementClusterContainerKubeconfig", &constants.OutputPathManagementClusterContainerKubeconfig, home + "/kubeconfigs/management/container.yaml"},
		{"OutputPathMainClusterKubeconfig", &constants.OutputPathMainClusterKubeconfig, home + "/kubeconfigs/main.yaml"},
		{"OutputPathJWKSDocument", &constants.OutputPathJWKSDocument, home + "/workload-identity/openid-provider/jwks.json"},
	}
	for _, tc := range tests {
		if *tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, *tc.got, tc.want)
		}
	}
}

// Mutates globals and parsed config; not parallel. XDG_CONFIG_HOME is
// pointed at a temp dir so clusterdir resolves under the test's control.
func TestOutputsHome(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	origParsed := config.ParsedGeneralConfig
	origConfigsDirectory := globals.ConfigsDirectory
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origParsed
		globals.ConfigsDirectory = origConfigsDirectory
	})

	setParsedName := func(name string) {
		parsed := &config.GeneralConfig{}
		parsed.Cluster.Name = name
		config.ParsedGeneralConfig = parsed
	}

	operatorDirectory := filepath.Join(t.TempDir(), "customer-a")

	tests := []struct {
		name             string
		parsedName       string
		configsDirectory string
		wantHome         string
		wantErr          bool
	}{
		{
			name:             "per-cluster configs directory means the cluster home",
			parsedName:       "prod",
			configsDirectory: filepath.Join(configHome, "kubeaid-cli", "prod", "configs"),
			wantHome:         filepath.Join(configHome, "kubeaid-cli", "prod"),
		},
		{
			name: "general.yaml disagreeing with its directory follows the directory",
			// The directory was chosen — via --cluster-name, the token
			// flow, or the operator typing the path — so a copied config
			// inside it does not redirect the outputs. The disagreement is
			// reported at ERROR.
			parsedName:       "bar",
			configsDirectory: filepath.Join(configHome, "kubeaid-cli", "foo", "configs"),
			wantHome:         filepath.Join(configHome, "kubeaid-cli", "foo"),
		},
		{
			name: "case-insensitive name agreement is not a mismatch",
			// EqualFold: on macOS/Windows "PROD" and "prod" are one
			// directory; the home follows the directory's spelling.
			parsedName:       "PROD",
			configsDirectory: filepath.Join(configHome, "kubeaid-cli", "prod", "configs"),
			wantHome:         filepath.Join(configHome, "kubeaid-cli", "prod"),
		},
		{
			name: "operator's own directory is the home, per-user root not involved",
			// Everything the run produces lands next to the config files
			// the operator pointed at.
			parsedName:       "prod",
			configsDirectory: operatorDirectory,
			wantHome:         operatorDirectory,
		},
		{
			name:             "uncleaned operator directory is cleaned",
			parsedName:       "prod",
			configsDirectory: operatorDirectory + string(filepath.Separator),
			wantHome:         operatorDirectory,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setParsedName(tc.parsedName)
			globals.ConfigsDirectory = tc.configsDirectory

			home, err := outputsHome(t.Context())

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if home != tc.wantHome {
				t.Fatalf("home = %q, want %q", home, tc.wantHome)
			}
		})
	}
}

// Mutates globals and parsed config; not parallel. An explicit
// --configs-directory works without HOME/XDG_CONFIG_HOME: the operator's
// directory is the home, and the per-user root is never consulted.
func TestOutputsHomeWithoutUserConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	origParsed := config.ParsedGeneralConfig
	origConfigsDirectory := globals.ConfigsDirectory
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origParsed
		globals.ConfigsDirectory = origConfigsDirectory
	})
	parsed := &config.GeneralConfig{}
	parsed.Cluster.Name = "prod"
	config.ParsedGeneralConfig = parsed
	operatorDirectory := filepath.Join(t.TempDir(), "customer-a")
	globals.ConfigsDirectory = operatorDirectory

	home, err := outputsHome(t.Context())
	if err != nil {
		t.Fatalf("an explicit directory must work without HOME: %v", err)
	}
	if home != operatorDirectory {
		t.Fatalf("home = %q, want %q", home, operatorDirectory)
	}
}
