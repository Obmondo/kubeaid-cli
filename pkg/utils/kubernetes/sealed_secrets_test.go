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
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "release already deployed -- skips install",
			releases: []*release.Release{
				makeRelease(
					constants.ReleaseNameSealedSecrets,
					constants.NamespaceSealedSecrets,
					release.StatusDeployed,
				),
			},
			wantInstalled: false,
		},
		{
			name:          "no existing release -- fresh install succeeds",
			releases:      nil,
			chartToLoad:   minimalChart(),
			wantInstalled: true,
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
			factory := &fakeHelmFactory{
				lister:      singleResponseLister(tc.releases),
				installer:   installer,
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

// ── SealIfPlaintextChanged ───────────────────────────────────────────────────

// Pins the cert-folded cache invariant: the kubeaid-sha256 header on
// line 1 of each rendered SealedSecret file mixes plaintext AND the
// controller's cert bytes, so a re-keyed controller invalidates the
// cache. Regressions in that combination would silently re-surface as
// "no key could decrypt secret" mid-bootstrap.
func TestSealIfPlaintextChanged(t *testing.T) {
	origNewKubesealClientConfig := newKubesealClientConfigFn
	origOpenCert := openCertFn
	origParseKey := parseKeyFn
	origSeal := sealFn
	t.Cleanup(func() {
		newKubesealClientConfigFn = origNewKubesealClientConfig
		openCertFn = origOpenCert
		parseKeyFn = origParseKey
		sealFn = origSeal
	})

	// header builds a "# kubeaid-sha256: <hex>" line over the provided
	// inputs. When called with just plaintext it reproduces the
	// pre-fix (plaintext-only) header that older kubeaid-cli versions
	// wrote — exercises the self-healing upgrade path.
	header := func(parts ...[]byte) string {
		return "# kubeaid-sha256: " + sha256Hex(parts...) + "\n"
	}

	tests := []struct {
		name string

		// cert is what openCertFn returns. Different values across
		// runs simulate a re-keyed controller.
		cert string

		plaintext []byte

		// existingFile, when non-nil, is written to the destination
		// before the call so we can drive cache-hit / cache-miss
		// paths off the kubeaid-sha256 header on its first line.
		existingFile []byte

		// failOpenCert / failParseKey / failSeal force the matching
		// seam to return an error. Used by the error-path subtests.
		failOpenCert  bool
		failParseKey  bool
		failSeal      bool

		// wantSealCalls discriminates cache-hit (0) from cache-miss
		// (1) without having to peek at the sealed bytes themselves.
		wantSealCalls int

		// wantFileChanged: did SealIfPlaintextChanged rewrite the
		// destination? Cache hits must leave it byte-identical.
		wantFileChanged bool

		// wantHeaderParts, when non-nil, asserts the new kubeaid-sha256
		// header on the rewritten file is sha256Hex(parts...).
		wantHeaderParts [][]byte

		wantErr         bool
		wantErrContains string
	}{
		{
			name:            "cache hit -- same plaintext, same cert",
			cert:            "cert-v1",
			plaintext:       []byte("data1"),
			existingFile:    []byte(header([]byte("data1"), []byte("cert-v1")) + "sealed-with-cert-v1\n"),
			wantSealCalls:   0,
			wantFileChanged: false,
		},
		{
			name:            "cache miss -- destination absent",
			cert:            "cert-v1",
			plaintext:       []byte("data1"),
			existingFile:    nil,
			wantSealCalls:   1,
			wantFileChanged: true,
			wantHeaderParts: [][]byte{[]byte("data1"), []byte("cert-v1")},
		},
		{
			name:            "cache miss -- plaintext changed",
			cert:            "cert-v1",
			plaintext:       []byte("data2"),
			existingFile:    []byte(header([]byte("data1"), []byte("cert-v1")) + "sealed-old\n"),
			wantSealCalls:   1,
			wantFileChanged: true,
			wantHeaderParts: [][]byte{[]byte("data2"), []byte("cert-v1")},
		},
		{
			// Load-bearing case for the cert-fold invariant. If
			// the cache key dropped certBytes, this would be a
			// cache hit and the test would fail.
			name:            "cache miss -- controller re-keyed",
			cert:            "cert-v2",
			plaintext:       []byte("data1"),
			existingFile:    []byte(header([]byte("data1"), []byte("cert-v1")) + "sealed-with-cert-v1\n"),
			wantSealCalls:   1,
			wantFileChanged: true,
			wantHeaderParts: [][]byte{[]byte("data1"), []byte("cert-v2")},
		},
		{
			// Files written by pre-fix kubeaid-cli store
			// sha256(plaintext) only. On first run after upgrade
			// the new (plaintext, cert) hash mismatches, forcing
			// a one-time re-seal. Self-heal — operator does
			// nothing.
			name:            "cache miss -- legacy plaintext-only header",
			cert:            "cert-v1",
			plaintext:       []byte("data1"),
			existingFile:    []byte(header([]byte("data1")) + "sealed-legacy\n"),
			wantSealCalls:   1,
			wantFileChanged: true,
			wantHeaderParts: [][]byte{[]byte("data1"), []byte("cert-v1")},
		},
		{
			name:            "openCertFn returns error",
			cert:            "cert-v1",
			plaintext:       []byte("data1"),
			existingFile:    []byte(header([]byte("data1"), []byte("cert-v1")) + "untouched\n"),
			failOpenCert:    true,
			wantSealCalls:   0,
			wantFileChanged: false,
			wantErr:         true,
			wantErrContains: "reading sealed secrets controller's certificate",
		},
		{
			name:            "parseKeyFn returns error",
			cert:            "cert-v1",
			plaintext:       []byte("data1"),
			existingFile:    []byte(header([]byte("data1"), []byte("cert-v1")) + "untouched\n"),
			failParseKey:    true,
			wantSealCalls:   0,
			wantFileChanged: false,
			wantErr:         true,
			wantErrContains: "retrieving sealed secrets controller's public key",
		},
		{
			// Cache miss reached, seal step fails. renameio's
			// atomicity guarantee means the destination is
			// untouched even though we got past the cache check.
			name:            "sealFn returns error -- file untouched",
			cert:            "cert-v2",
			plaintext:       []byte("data1"),
			existingFile:    []byte(header([]byte("data1"), []byte("cert-v1")) + "sealed-with-cert-v1\n"),
			failSeal:        true,
			wantSealCalls:   1,
			wantFileChanged: false,
			wantErr:         true,
			wantErrContains: "encrypting secret",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newKubesealClientConfigFn = func() clientcmd.ClientConfig { return nil }
			openCertFn = func(_ context.Context, _ kubeseal.ClientConfig, _, _, _ string) (io.ReadCloser, error) {
				if tc.failOpenCert {
					return nil, errors.New("cert unavailable")
				}
				return io.NopCloser(strings.NewReader(tc.cert)), nil
			}
			parseKeyFn = func(_ io.Reader) (*rsa.PublicKey, error) {
				if tc.failParseKey {
					return nil, errors.New("bad key format")
				}
				return &rsa.PublicKey{}, nil
			}
			sealCalls := 0
			sealFn = func(_ kubeseal.ClientConfig, _ string, _ io.Reader, out io.Writer,
				_ *rsa.PublicKey, _ sealedSecretsV1Aplha1.SealingScope,
			) error {
				sealCalls++
				if tc.failSeal {
					return errors.New("encryption failed")
				}
				_, err := out.Write([]byte("fresh-ciphertext-for-" + tc.cert + "\n"))
				return err
			}

			dest := filepath.Join(t.TempDir(), "secret.yaml")
			if tc.existingFile != nil {
				require.NoError(t, os.WriteFile(dest, tc.existingFile, 0o600))
			}

			err := SealIfPlaintextChanged(context.Background(), dest, tc.plaintext)

			assert.Equal(t, tc.wantSealCalls, sealCalls, "sealFn call count")

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
			} else {
				require.NoError(t, err)
			}

			if tc.existingFile == nil && !tc.wantFileChanged {
				// File-absent + cache-hit not a real combination;
				// short-circuit so we don't try to read a file we
				// never wrote.
				return
			}

			gotContent, readErr := os.ReadFile(dest)
			if tc.existingFile == nil && tc.wantErr {
				// Error before write: file should not exist at all.
				require.True(t, os.IsNotExist(readErr), "file should not have been created")
				return
			}
			require.NoError(t, readErr)

			if tc.wantFileChanged {
				assert.NotEqual(t, tc.existingFile, gotContent, "destination should have been rewritten")
				gotHeader := strings.SplitN(string(gotContent), "\n", 2)[0]
				wantHeader := "# kubeaid-sha256: " + sha256Hex(tc.wantHeaderParts...)
				assert.Equal(t, wantHeader, gotHeader, "new header hash must mix plaintext and current cert")
			} else {
				assert.Equal(t, tc.existingFile, gotContent, "destination must be byte-identical on cache hit / pre-write error")
			}
		})
	}
}
