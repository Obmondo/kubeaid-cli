// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package obmondo

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

const testToken = "tok_9f3c1a7e5b2d4088"

// testCertPEM mints a self-signed certificate carrying cn as its Subject
// Common Name. Real x509 rather than a fixture string, because what is under
// test is that the CN is read out of the certificate itself.
func testCertPEM(t *testing.T, cn string) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func okEnvelope(data string) string {
	return `{"status":200,"success":true,"data":` + data + `,"message":"ok","error_text":""}`
}

const testConfigData = `{
  "general_yaml": "cluster:\n  name: demo\n",
  "secrets_yaml": "hetzner:\n  apiToken: \"tok\"\n"
}`

func TestFetchReturnsTheConfigAndSendsTheTokenBothWays(t *testing.T) {
	var gotHeader, gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("token")
		gotQuery = r.URL.Query().Get("token")
		_, _ = w.Write([]byte(okEnvelope(testConfigData)))
	}))
	defer server.Close()

	config, err := Fetch(context.Background(), server.URL, testToken)
	require.NoError(t, err)
	require.Equal(t, "cluster:\n  name: demo\n", config.GeneralYAML)
	require.Contains(t, config.SecretsYAML, "apiToken")
	require.Nil(t, config.Puppet)

	// The header is what keeps the token out of intermediary request logs;
	// the query parameter is what the portal's endpoint reads.
	require.Equal(t, testToken, gotHeader)
	require.Equal(t, testToken, gotQuery)
}

// A token that will not work — expired, already used, or simply wrong — must
// produce one actionable message rather than leaking which case applied.
func TestFetchTreatsEveryDeadTokenTheSame(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusGone} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		_, err := Fetch(context.Background(), server.URL, testToken)
		server.Close()

		require.Errorf(t, err, "status %d", status)
		require.Containsf(t, err.Error(), "generate a new one", "status %d", status)
	}
}

// No error path may echo the token: these strings reach logs and terminals.
func TestFetchNeverPutsTheTokenInAnError(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"unreadable":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) },
		"refused": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"success":false,"error_text":"install link is invalid"}`))
		},
		"no general": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(okEnvelope(`{"general_yaml":"","secrets_yaml":"x"}`)))
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()

			_, err := Fetch(context.Background(), server.URL, testToken)
			require.Error(t, err)
			require.NotContains(t, err.Error(), testToken)
		})
	}

	// An unreachable host is the case most likely to embed the URL, and the
	// URL carries the token in its query string.
	_, err := Fetch(context.Background(), "http://127.0.0.1:1", testToken)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
}

// The CN inside the certificate is the certname bootstrap renders into
// cluster-vars.jsonnet, so it — not the JSON field, and not the cluster's
// name — is what the CLI must carry forward.
func TestFetchTakesTheCertnameFromTheCertificateCN(t *testing.T) {
	// The JSON field disagrees with the CN, and the cluster is named
	// something else again. All three are allowed to differ.
	body := okEnvelope(`{
	  "general_yaml": "cluster:\n  name: demo-01\n",
	  "secrets_yaml": "hetzner:\n  apiToken: \"tok\"\n",
	  "puppet": {"certname": "stale-echo", "certificate": ` +
		strconv.Quote(testCertPEM(t, "k8s-demo.acme")) + `, "private_key": "k"}
	}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	config, err := Fetch(context.Background(), server.URL, testToken)
	require.NoError(t, err)
	require.Equal(t, "k8s-demo.acme", config.Puppet.Certname)
}

// An unusable certificate must fail now, not minutes into bootstrap when
// cert.ReadCN hits it — by then the single-use token is already spent.
func TestFetchRejectsAnUnparseableCertificate(t *testing.T) {
	body := okEnvelope(`{
	  "general_yaml": "cluster:\n  name: demo-01\n",
	  "secrets_yaml": "x",
	  "puppet": {"certname": "demo-01.acme", "certificate": "not a pem", "private_key": "k"}
	}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL, testToken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unusable Puppet certificate")
	require.NotContains(t, err.Error(), testToken)
}

// Monitoring off means no Puppet block at all, which is not an error.
func TestFetchAllowsAMissingPuppetBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(okEnvelope(testConfigData)))
	}))
	defer server.Close()

	config, err := Fetch(context.Background(), server.URL, testToken)
	require.NoError(t, err)
	require.Nil(t, config.Puppet)
}

func TestFetchRejectsAnEmptyToken(t *testing.T) {
	_, err := Fetch(context.Background(), "https://example.invalid", "   ")
	require.Error(t, err)
}

func TestClusterNameReadsTheNameAndToleratesUnknownFields(t *testing.T) {
	name, err := ClusterName("cluster:\n  name: demo-01\n  k8sVersion: v1.35.7\nsomethingNewer: true\n")
	require.NoError(t, err)
	require.Equal(t, "demo-01", name)

	_, err = ClusterName("cluster:\n  k8sVersion: v1.35.7\n")
	require.Error(t, err)

	_, err = ClusterName("this: is: not: yaml:\n  - [")
	require.Error(t, err)
}

func TestWriteLaysOutTheFilesWithRestrictivePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")

	written, err := Write(dir, &ClusterConfig{
		GeneralYAML: "cluster:\n  name: demo\n",
		SecretsYAML: "hetzner:\n  apiToken: \"tok\"\n",
	})
	require.NoError(t, err)

	general, err := os.ReadFile(written.General)
	require.NoError(t, err)
	require.Equal(t, "cluster:\n  name: demo\n", string(general))

	// secrets.yaml carries cloud credentials, so it must not be group- or
	// world-readable.
	info, err := os.Stat(written.Secrets)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
}

// The token is single-use, so an existing general.yaml means either a previous
// run succeeded or unrelated config lives here. Overwriting either silently
// would destroy work.
func TestWriteRefusesToClobberExistingConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "general.yaml"), []byte("existing"), 0o600))

	_, err := Write(dir, &ClusterConfig{GeneralYAML: "cluster:\n  name: demo\n"})
	require.Error(t, err)

	// Must name the directory it actually looked in. Telling the operator to
	// "re-run without --connect-obmondo" would send bootstrap to
	// outputs/configs, which is not where this file is.
	require.Contains(t, err.Error(), "--configs-directory "+dir)

	kept, err := os.ReadFile(filepath.Join(dir, "general.yaml"))
	require.NoError(t, err)
	require.Equal(t, "existing", string(kept))
}

func TestWriteStoresPuppetMaterialAndPointsGeneralAtIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")

	written, err := Write(dir, &ClusterConfig{
		GeneralYAML: "cluster:\n  name: demo\nobmondo:\n  customerID: acme\n  monitoring: true\n  certPath: \"\"\n  keyPath: \"\"\n",
		SecretsYAML: "hetzner:\n  apiToken: \"tok\"\n",
		Puppet: &PuppetMaterial{
			Certname:      "demo.acme",
			PrivateKey:    "-----BEGIN RSA PRIVATE KEY-----\nAAA\n-----END RSA PRIVATE KEY-----\n",
			Certificate:   "-----BEGIN CERTIFICATE-----\nBBB\n-----END CERTIFICATE-----\n",
			CACertificate: "-----BEGIN CERTIFICATE-----\nCCC\n-----END CERTIFICATE-----\n",
		},
	})
	require.NoError(t, err)

	// The private key is as sensitive as secrets.yaml; the certificate and CA
	// are public halves.
	keyInfo, err := os.Stat(written.KeyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), keyInfo.Mode().Perm())

	general, err := os.ReadFile(written.General)
	require.NoError(t, err)

	// The portal cannot know these paths, so the CLI fills them in.
	require.Contains(t, string(general), "certPath: \""+written.CertPath+"\"")
	require.Contains(t, string(general), "keyPath: \""+written.KeyPath+"\"")
	require.Contains(t, string(general), "customerID: acme")
}

func TestWriteSkipsPuppetPathsWhenMonitoringIsOff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")

	written, err := Write(dir, &ClusterConfig{
		GeneralYAML: "cluster:\n  name: demo\n",
		SecretsYAML: "hetzner:\n  apiToken: \"tok\"\n",
	})
	require.NoError(t, err)
	require.Empty(t, written.CertPath)

	_, err = os.Stat(filepath.Join(dir, obmondoDirName))
	require.True(t, os.IsNotExist(err), "no obmondo directory should be created")
}

// The notice is the only place the operator is told where secrets.yaml went:
// LogPaths writes at INFO, which the default handler sends to the log file
// rather than the terminal.
func TestRenderSecretsNoticeNamesEveryFileItWroteAndSaysToBackThemUp(t *testing.T) {
	got := renderSecretsNotice(&WrittenPaths{
		General:  "/cfg/general.yaml",
		Secrets:  "/cfg/secrets.yaml",
		KeyPath:  "/cfg/obmondo/key.pem",
		CertPath: "/cfg/obmondo/cert.pem",
	})

	require.Contains(t, got, "/cfg/secrets.yaml")
	require.Contains(t, got, "/cfg/obmondo/key.pem")
	require.Contains(t, got, "pass repo")

	// The public halves are not a backup concern and would only pad the box.
	require.NotContains(t, got, "cert.pem")
	require.NotContains(t, got, "general.yaml")
}

func TestRenderSecretsNoticeOmitsTheKeyLineWhenMonitoringIsOff(t *testing.T) {
	got := renderSecretsNotice(&WrittenPaths{Secrets: "/cfg/secrets.yaml"})

	require.Contains(t, got, "/cfg/secrets.yaml")
	require.NotContains(t, got, "key.pem")
}

// The prose must not widen the box on its own: 80 columns is the floor the
// other boxed surfaces are built for. A long path may still push the border
// past the viewport, which is the deliberate tradeoff below.
func TestRenderSecretsNoticeProseFitsAnEightyColumnTerminal(t *testing.T) {
	got := renderSecretsNotice(&WrittenPaths{Secrets: "/c/secrets.yaml"})

	for _, line := range strings.Split(got, "\n") {
		require.LessOrEqual(t, lipgloss.Width(line), 80, "line too wide: %q", line)
	}
}

// Paths are never wrapped to fit, matching renderPRMergeBox: a path split
// across two lines cannot be copy-pasted, which is the whole point of
// printing it.
func TestRenderSecretsNoticeNeverWrapsALongPath(t *testing.T) {
	base := "/home/a-long-user-name/.config/kubeaid-cli/production-cluster-01/configs"

	got := renderSecretsNotice(&WrittenPaths{
		Secrets: base + "/secrets.yaml",
		KeyPath: base + "/obmondo/key.pem",
	})

	require.Contains(t, got, base+"/secrets.yaml")
	require.Contains(t, got, base+"/obmondo/key.pem")
}

// bootstrap always calls this, including on the plain `config generate` path
// where nothing was fetched — so a nil must be a no-op, not a panic.
func TestPrintSecretsNoticeIsANoOpWithNothingToReport(t *testing.T) {
	require.NotPanics(t, func() {
		PrintSecretsNotice(context.Background(), nil)
		PrintSecretsNotice(context.Background(), &WrittenPaths{})
	})
}

// The substitution is line-based, so it must not touch a certPath belonging to
// some other block — this is the failure mode that would silently corrupt an
// unrelated part of the config.
func TestWithObmondoCertPathsOnlyEditsTheObmondoBlock(t *testing.T) {
	input := strings.Join([]string{
		"cloud:",
		"  hetzner:",
		"    certPath: /should/not/change",
		"    keyPath: /should/not/change",
		"obmondo:",
		"  customerID: acme",
		"  certPath: \"\"",
		"  keyPath: \"\"",
		"cluster:",
		"  certPath: /also/untouched",
		"",
	}, "\n")

	got := withObmondoCertPaths(input, "/new/cert.pem", "/new/key.pem")

	require.Contains(t, got, "    certPath: /should/not/change")
	require.Contains(t, got, "    keyPath: /should/not/change")
	require.Contains(t, got, "  certPath: /also/untouched")
	require.Contains(t, got, "  certPath: \"/new/cert.pem\"")
	require.Contains(t, got, "  keyPath: \"/new/key.pem\"")
	require.Contains(t, got, "  customerID: acme")

	// No obmondo block at all is a no-op, not a panic or an appended key.
	require.Equal(t, "cluster:\n  name: demo\n",
		withObmondoCertPaths("cluster:\n  name: demo\n", "/c", "/k"))
}
