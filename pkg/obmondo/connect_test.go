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

const (
	testToken = "tok_9f3c1a7e5b2d4088"

	// testCertname is the <cluster>.<customer-id> the token was issued for.
	// Sent on every redemption: the API refuses one whose certname disagrees
	// with the token's.
	testCertname = "demo.acme"
)

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
	var gotHeader, gotQuery, gotCertname string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("token")
		gotQuery = r.URL.Query().Get("token")
		gotCertname = r.URL.Query().Get("certname")
		_, _ = w.Write([]byte(okEnvelope(testConfigData)))
	}))
	defer server.Close()

	config, err := Fetch(context.Background(), server.URL, testToken, testCertname)
	require.NoError(t, err)
	require.Equal(t, "cluster:\n  name: demo\n", config.GeneralYAML)
	require.Contains(t, config.SecretsYAML, "apiToken")
	require.Nil(t, config.Puppet)

	// The header is what keeps the token out of intermediary request logs;
	// the query parameter is what the portal's endpoint reads.
	require.Equal(t, testToken, gotHeader)
	require.Equal(t, testToken, gotQuery)

	// The API gates redemption on this matching the token's own certname, so
	// omitting it is a validation failure rather than a permissive default.
	require.Equal(t, testCertname, gotCertname)
}

// A token that will not work — expired, simply wrong, or issued for another
// cluster — must produce one actionable message rather than leaking which
// case applied.
func TestFetchTreatsEveryDeadTokenTheSame(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusGone} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		_, err := Fetch(context.Background(), server.URL, testToken, testCertname)
		server.Close()

		require.Errorf(t, err, "status %d", status)
		require.Containsf(t, err.Error(), "generate a new link", "status %d", status)
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

			_, err := Fetch(context.Background(), server.URL, testToken, testCertname)
			require.Error(t, err)
			require.NotContains(t, err.Error(), testToken)
		})
	}

	// An unreachable host is the case most likely to embed the URL, and the
	// URL carries the token in its query string.
	_, err := Fetch(context.Background(), "http://127.0.0.1:1", testToken, testCertname)
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

	config, err := Fetch(context.Background(), server.URL, testToken, testCertname)
	require.NoError(t, err)
	require.Equal(t, "k8s-demo.acme", config.Puppet.Certname)
}

// An unusable certificate must fail now, not minutes into bootstrap when
// cert.ReadCN hits it — by then a lot of work has happened on a config that
// was never going to authenticate.
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

	_, err := Fetch(context.Background(), server.URL, testToken, testCertname)
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

	config, err := Fetch(context.Background(), server.URL, testToken, testCertname)
	require.NoError(t, err)
	require.Nil(t, config.Puppet)
}

func TestFetchRejectsAnEmptyToken(t *testing.T) {
	_, err := Fetch(context.Background(), "https://example.invalid", "   ", testCertname)
	require.Error(t, err)
}

// Caught before the request rather than as a 406 from the API, so the
// operator is told which value is missing instead of reading a validation
// failure back out of an envelope.
func TestFetchRejectsAnEmptyCertname(t *testing.T) {
	_, err := Fetch(context.Background(), "https://example.invalid", testToken, "   ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "certname")
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

// A token is redeemable for its whole window, so a re-run reaching Write has
// deliberately asked for this certname's config again. Replacing what is
// there is the intent — prompting would ask the same question every re-run.
func TestWriteOverwritesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "general.yaml"), []byte("stale"), 0o600))

	written, err := Write(dir, &ClusterConfig{
		GeneralYAML: "cluster:\n  name: demo\n",
		SecretsYAML: "hetzner:\n  apiToken: tok\n",
	})
	require.NoError(t, err)

	replaced, err := os.ReadFile(written.General)
	require.NoError(t, err)
	require.Equal(t, "cluster:\n  name: demo\n", string(replaced))
}

// A directory missing any one of the five files cannot bootstrap, so a
// partial set must read as absent rather than as something to work with.
func TestCompleteRequiresEveryFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")
	written, err := Write(dir, &ClusterConfig{
		GeneralYAML: "cluster:\n  name: demo\n",
		SecretsYAML: "x",
		Puppet: &PuppetMaterial{
			Certificate:   testCertPEM(t, testCertname),
			PrivateKey:    "k",
			CACertificate: "ca",
		},
	})
	require.NoError(t, err)

	_, complete := Complete(dir)
	require.True(t, complete)

	require.NoError(t, os.Remove(written.KeyPath))
	_, complete = Complete(dir)
	require.False(t, complete, "a missing private key must make the set incomplete")
}

// The certificate's CN is what ties a directory to a certname — the
// directory's own name proves nothing, since a cluster's name and its
// certname are separate identifiers.
func TestVerifyOnDiskMatchesOnTheCertificateCN(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "configs")
	_, err := Write(dir, &ClusterConfig{
		GeneralYAML: "cluster:\n  name: something-else\n",
		SecretsYAML: "x",
		Puppet: &PuppetMaterial{
			Certificate:   testCertPEM(t, testCertname),
			PrivateKey:    "k",
			CACertificate: "ca",
		},
	})
	require.NoError(t, err)

	paths, err := VerifyOnDisk(dir, testCertname)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "general.yaml"), paths.General)

	_, err = VerifyOnDisk(dir, "other.acme")
	require.Error(t, err)
	require.Contains(t, err.Error(), testCertname)
}

func TestVerifyOnDiskRejectsAnIncompleteDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "general.yaml"), []byte("cluster:\n  name: demo\n"), 0o600))

	_, err := VerifyOnDisk(dir, testCertname)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--token")
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

// realisticGeneralYAML mirrors the shape pkg/render produces: four lines
// called privateKeyFilePath, at four depths, under three top-level keys.
func realisticGeneralYAML() string {
	return strings.Join([]string{
		"git:",
		"  sshUsername: git",
		"  useSSHAgent: false",
		"  privateKeyFilePath: \"\"",
		"cluster:",
		"  name: demo",
		"  argoCD:",
		"    deployKeys:",
		"      kubeaid:",
		"        privateKeyFilePath: \"\"",
		"      kubeaidConfig:",
		"        privateKeyFilePath: \"\"",
		"cloud:",
		"  hetzner:",
		"    mode: hcloud",
		"    sshKeyPair:",
		"      name: demo",
		"      privateKeyFilePath: \"\"",
		"",
	}, "\n")
}

// TestWithSSHKeyPathsFillsEachSlotFromItsOwnPurpose is the whole point of
// matching on the path rather than the field name: the operator key and the
// deploy key both land in lines called privateKeyFilePath, and swapping them
// yields a config that parses, bootstraps, and then reaches nothing.
func TestWithSSHKeyPathsFillsEachSlotFromItsOwnPurpose(t *testing.T) {
	got := withSSHKeyPaths(realisticGeneralYAML(), map[string]string{
		sshKeyPurposeOperator: "/configs/obmondo/operator-key",
		sshKeyPurposeDeploy:   "/configs/obmondo/deploy-key",
	})

	// git and the Hetzner key pair are the same person, so the same key.
	require.Contains(t, got, "  privateKeyFilePath: \"/configs/obmondo/operator-key\"")
	require.Contains(t, got, "      privateKeyFilePath: \"/configs/obmondo/operator-key\"")

	// Both ArgoCD deploy keys take the deploy key.
	require.Equal(t, 2, strings.Count(got, "        privateKeyFilePath: \"/configs/obmondo/deploy-key\""))

	// Nothing was left empty, and nothing was crossed over.
	require.NotContains(t, got, "privateKeyFilePath: \"\"")
	require.NotContains(t, got, "        privateKeyFilePath: \"/configs/obmondo/operator-key\"")

	// Indentation is preserved, so the result is still valid YAML.
	require.Contains(t, got, "  sshUsername: git")
	require.Contains(t, got, "    mode: hcloud")
}

// TestWithSSHKeyPathsLeavesUndeliveredSlotsAlone covers the operator who
// named their own key or works through an agent: the portal sends nothing for
// that purpose and whatever general.yaml already said must survive.
func TestWithSSHKeyPathsLeavesUndeliveredSlotsAlone(t *testing.T) {
	input := strings.ReplaceAll(realisticGeneralYAML(),
		"  privateKeyFilePath: \"\"", "  privateKeyFilePath: ~/.ssh/id_ed25519")

	got := withSSHKeyPaths(input, map[string]string{
		sshKeyPurposeDeploy: "/configs/obmondo/deploy-key",
	})

	require.Contains(t, got, "  privateKeyFilePath: ~/.ssh/id_ed25519")
	require.Equal(t, 2, strings.Count(got, "        privateKeyFilePath: \"/configs/obmondo/deploy-key\""))
}

// TestWithSSHKeyPathsIgnoresLookalikePaths guards the failure a flat key
// match would cause: a privateKeyFilePath somewhere we are not authoritative
// about must be left exactly as it arrived.
func TestWithSSHKeyPathsIgnoresLookalikePaths(t *testing.T) {
	input := strings.Join([]string{
		"cloud:",
		"  azure:",
		"    workloadIdentity:",
		"      openIDProviderSSHKeyPair:",
		"        privateKeyFilePath: /azure/only",
		"cluster:",
		"  hosts:",
		"    - name: bm1",
		"      ssh:",
		"        privateKeyFilePath: /host/only",
		"",
	}, "\n")

	got := withSSHKeyPaths(input, map[string]string{
		sshKeyPurposeOperator: "/configs/obmondo/operator-key",
		sshKeyPurposeDeploy:   "/configs/obmondo/deploy-key",
	})

	require.Equal(t, input, got, "paths we do not own must be untouched")
}
