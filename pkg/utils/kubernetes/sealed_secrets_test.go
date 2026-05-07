// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"crypto/rsa"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sealedSecretsV1Aplha1 "github.com/bitnami-labs/sealed-secrets/pkg/apis/sealedsecrets/v1alpha1"
	"github.com/bitnami-labs/sealed-secrets/pkg/kubeseal"
	"github.com/google/renameio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
)

// ── installSealedSecretsWithFactory ──────────────────────────────────────────

// Mutates config.ParsedGeneralConfig.Forks.KubeaidFork.ParsedURL -- sequential only.
func TestInstallSealedSecretsWithFactory(t *testing.T) {
	orig := config.ParsedGeneralConfig.Forks.KubeaidFork.ParsedURL
	t.Cleanup(func() { config.ParsedGeneralConfig.Forks.KubeaidFork.ParsedURL = orig })
	config.ParsedGeneralConfig.Forks.KubeaidFork.ParsedURL = &giturl.ParsedURL{
		Host:  "github.com",
		Owner: "Obmondo",
		Repo:  "KubeAid",
	}

	tests := []struct {
		name            string
		releases        []*release.Release
		chartToLoad     *chart.Chart
		chartErr        error
		installErr      error
		wantInstalled   bool
		wantUninstalled bool
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "release already deployed -- skips install and uninstall",
			releases: []*release.Release{
				makeRelease(
					constants.ReleaseNameSealedSecrets,
					constants.NamespaceSealedSecrets,
					release.StatusDeployed,
				),
			},
			wantInstalled:   false,
			wantUninstalled: false,
		},
		{
			name:            "no existing release -- fresh install succeeds",
			releases:        nil,
			chartToLoad:     minimalChart(),
			wantInstalled:   true,
			wantUninstalled: false,
		},
		{
			name:            "LoadChart fails -- propagates error",
			releases:        nil,
			chartErr:        errors.New("corrupt chart"),
			wantErr:         true,
			wantErrContains: "failed loading helm chart",
		},
		{
			name:            "install fails -- propagates error",
			releases:        nil,
			chartToLoad:     minimalChart(),
			installErr:      errors.New("network timeout"),
			wantInstalled:   true,
			wantErr:         true,
			wantErrContains: "failed installing helm chart",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installer := &fakeInstallRunner{err: tc.installErr}
			uninstaller := &fakeUninstallRunner{}
			factory := &fakeHelmFactory{
				lister:      singleResponseLister(tc.releases),
				installer:   installer,
				uninstaller: uninstaller,
				chartToLoad: tc.chartToLoad,
				chartErr:    tc.chartErr,
			}

			err := installSealedSecretsWithFactory(context.Background(), factory)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantInstalled, installer.called)
			assert.Equal(t, tc.wantUninstalled, uninstaller.called)
		})
	}
}

// ── GenerateSealedSecret ────────────────────────────────────────────────────

// Mutates newKubesealClientConfigFn, openCertFn, parseKeyFn, sealFn,
// renameTempFileFn — sequential only.
func TestGenerateSealedSecret(t *testing.T) {
	origNewKubesealClientConfig := newKubesealClientConfigFn
	origOpenCert := openCertFn
	origParseKey := parseKeyFn
	origSeal := sealFn
	origRenameTempFile := renameTempFileFn
	t.Cleanup(func() {
		newKubesealClientConfigFn = origNewKubesealClientConfig
		openCertFn = origOpenCert
		parseKeyFn = origParseKey
		sealFn = origSeal
		renameTempFileFn = origRenameTempFile
	})

	tests := []struct {
		name            string
		setupSecretFile func(t *testing.T) string
		openCert        func(ctx context.Context, clientConfig kubeseal.ClientConfig, controllerNs, controllerName, certURL string) (io.ReadCloser, error)
		parseKey        func(r io.Reader) (*rsa.PublicKey, error)
		seal            func(clientConfig kubeseal.ClientConfig, outputFormat string, in io.Reader, out io.Writer, pubKey *rsa.PublicKey, scope sealedSecretsV1Aplha1.SealingScope) error
		renameTempFile  func(dir, path string) (*renameio.PendingFile, error)
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "happy path -- all seams succeed",
			setupSecretFile: func(t *testing.T) string {
				t.Helper()
				p := filepath.Join(t.TempDir(), "secret.yaml")
				require.NoError(t, os.WriteFile(p, []byte("apiVersion: v1\nkind: Secret\n"), 0o600))
				return p
			},
			openCert: func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("fake-cert")), nil
			},
			parseKey: func(_ io.Reader) (*rsa.PublicKey, error) {
				return &rsa.PublicKey{}, nil
			},
			seal: func(_ kubeseal.ClientConfig, _ string, _ io.Reader, out io.Writer, _ *rsa.PublicKey, _ sealedSecretsV1Aplha1.SealingScope) error {
				_, err := out.Write([]byte("sealed-data"))
				return err
			},
			renameTempFile: func(_, path string) (*renameio.PendingFile, error) {
				return renameio.TempFile(t.TempDir(), path)
			},
		},
		{
			name: "openCert fails",
			setupSecretFile: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "unused.yaml")
			},
			openCert: func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				return nil, errors.New("cert unavailable")
			},
			wantErr:         true,
			wantErrContains: "failed reading sealed secrets controller's certificate",
		},
		{
			name: "parseKey fails",
			setupSecretFile: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "unused.yaml")
			},
			openCert: func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("fake-cert")), nil
			},
			parseKey: func(_ io.Reader) (*rsa.PublicKey, error) {
				return nil, errors.New("bad key format")
			},
			wantErr:         true,
			wantErrContains: "failed retrieving the sealed secrets controller's public key",
		},
		{
			name: "secret file does not exist",
			setupSecretFile: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent.yaml")
			},
			openCert: func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("fake-cert")), nil
			},
			parseKey: func(_ io.Reader) (*rsa.PublicKey, error) {
				return &rsa.PublicKey{}, nil
			},
			wantErr:         true,
			wantErrContains: "failed opening secret file",
		},
		{
			name: "renameTempFile fails",
			setupSecretFile: func(t *testing.T) string {
				t.Helper()
				p := filepath.Join(t.TempDir(), "secret.yaml")
				require.NoError(t, os.WriteFile(p, []byte("apiVersion: v1\nkind: Secret\n"), 0o600))
				return p
			},
			openCert: func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("fake-cert")), nil
			},
			parseKey: func(_ io.Reader) (*rsa.PublicKey, error) {
				return &rsa.PublicKey{}, nil
			},
			renameTempFile: func(_, _ string) (*renameio.PendingFile, error) {
				return nil, errors.New("disk full")
			},
			wantErr:         true,
			wantErrContains: "failed creating temporary sealed-secret file",
		},
		{
			name: "seal fails",
			setupSecretFile: func(t *testing.T) string {
				t.Helper()
				p := filepath.Join(t.TempDir(), "secret.yaml")
				require.NoError(t, os.WriteFile(p, []byte("apiVersion: v1\nkind: Secret\n"), 0o600))
				return p
			},
			openCert: func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("fake-cert")), nil
			},
			parseKey: func(_ io.Reader) (*rsa.PublicKey, error) {
				return &rsa.PublicKey{}, nil
			},
			seal: func(_ kubeseal.ClientConfig, _ string, _ io.Reader, _ io.Writer, _ *rsa.PublicKey, _ sealedSecretsV1Aplha1.SealingScope) error {
				return errors.New("encryption failed")
			},
			renameTempFile: func(_, path string) (*renameio.PendingFile, error) {
				return renameio.TempFile(t.TempDir(), path)
			},
			wantErr:         true,
			wantErrContains: "failed encrypting secret file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore globals for each subtest.
			origNewKC := newKubesealClientConfigFn
			origOC := openCertFn
			origPK := parseKeyFn
			origS := sealFn
			origRT := renameTempFileFn
			t.Cleanup(func() {
				newKubesealClientConfigFn = origNewKC
				openCertFn = origOC
				parseKeyFn = origPK
				sealFn = origS
				renameTempFileFn = origRT
			})

			newKubesealClientConfigFn = func() clientcmd.ClientConfig { return nil }

			if tc.openCert != nil {
				openCertFn = tc.openCert
			}
			if tc.parseKey != nil {
				parseKeyFn = tc.parseKey
			}
			if tc.seal != nil {
				sealFn = tc.seal
			}
			if tc.renameTempFile != nil {
				renameTempFileFn = tc.renameTempFile
			}

			secretPath := tc.setupSecretFile(t)

			err := GenerateSealedSecret(context.Background(), secretPath)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				return
			}
			require.NoError(t, err)
		})
	}
}
