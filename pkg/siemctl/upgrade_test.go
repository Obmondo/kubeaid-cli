// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/config/parser"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/core"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// The upgrade test renders the security operations stack from the previous
// release's cluster directory (testdata/upgrade/previous: its general config
// and everything that release rendered) with the current code, and checks:
//
//   - no credential rotates: every SealedSecret keeps its ciphertext, and every
//     password hash, cluster key and generated password in the rendered
//     values is still there;
//   - no tenant object disappears: every Application and SealedSecret of the
//     previous render is still rendered;
//   - the whole diff equals the reviewed golden file testdata/upgrade/current.diff.
//
// A deliberate render change: go test ./pkg/siemctl -run TestUpgradeRender -update
// and review current.diff. At a release, move the baseline forward:
// go test ./pkg/siemctl -run TestUpgradeRender -update-baseline (re-renders
// previous/ from testdata/upgrade/general.yaml, keeping its credentials).
var (
	updateUpgradeGolden   = flag.Bool("update", false, "rewrite testdata/upgrade/current.diff")
	updateUpgradeBaseline = flag.Bool("update-baseline", false, "re-render testdata/upgrade/previous")
)

const (
	upgradeDir      = "testdata/upgrade"
	upgradeBaseline = "testdata/upgrade/previous"
	upgradeGolden   = "testdata/upgrade/current.diff"
)

func TestUpgradeRender(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	withSealingCert(t)
	ctx := context.Background()

	if *updateUpgradeBaseline {
		require.NoError(t, os.MkdirAll(upgradeBaseline, 0o750))
		raw, err := os.ReadFile(filepath.Join(upgradeDir, "general.yaml"))
		require.NoError(t, err)
		//nolint:gosec // G703: the test's own testdata directory.
		require.NoError(t, os.WriteFile(filepath.Join(upgradeBaseline, "kubeaid-cli.general.yaml"), raw, 0o600))
		renderInto(t, upgradeBaseline)
	}

	work := filepath.Join(t.TempDir(), "k8s", "kubesoc")
	require.NoError(t, CopyTree(upgradeBaseline, work))
	renderInto(t, work)

	before := scanRendered(t, upgradeBaseline)
	after := scanRendered(t, work)
	require.NotEmpty(t, before.sealed, "the baseline has sealed Secrets")

	for id, data := range before.sealed {
		got, ok := after.sealed[id]
		if assert.True(t, ok, "SealedSecret %s was removed", id) {
			assert.True(t, data == got, "SealedSecret %s was re-encrypted: a credential rotated", id)
		}
	}
	for id, value := range before.credentials {
		if ns, ok := sealedInstead(id); ok {
			// The credential left the rendered values for a sealed Secret in
			// this release; the render must not keep a plaintext copy, and the
			// Secret must be there to carry it.
			assert.Empty(t, after.credentials[id], "credential %s is still in the values", id)
			assert.Contains(t, after.sealed, ns, "credential %s moved into no sealed Secret", id)
			continue
		}
		assert.True(t, value == after.credentials[id], "credential %s changed", id)
	}
	for app := range before.apps {
		assert.True(t, after.apps[app], "Application %s was removed", app)
	}
	for _, code := range []string{"001", "002", "demo"} {
		assert.True(t, after.apps["wazuh-"+code], "tenant %s has no Application", code)
	}

	diff, err := DiffTrees(ctx, upgradeBaseline, work)
	require.NoError(t, err)
	// Blob hashes depend on nothing but content, but keep the golden file
	// readable and stable across git versions.
	diff = stripIndexLines(diff)
	if *updateUpgradeGolden || *updateUpgradeBaseline {
		require.NoError(t, os.WriteFile(upgradeGolden, []byte(diff), 0o600))
	}
	golden, err := os.ReadFile(upgradeGolden)
	require.NoError(t, err, "run with -update to create %s", upgradeGolden)
	assert.Equal(t, string(golden), diff,
		"the render changed against the previous release: review it, then go test ./pkg/siemctl -run TestUpgradeRender -update")
}

// renderInto loads dir's general config and renders into dir, as
// `siem render` does.
func renderInto(t *testing.T, dir string) {
	t.Helper()
	origGeneral, origSecrets := config.ParsedGeneralConfig, config.ParsedSecretsConfig
	t.Cleanup(func() {
		config.ParsedGeneralConfig = origGeneral
		config.ParsedSecretsConfig = origSecrets
	})
	config.ParsedSecretsConfig = nil
	require.NoError(t, parser.LoadSecurityOperationsConfig(filepath.Join(dir, "kubeaid-cli.general.yaml")))
	_, err := core.RenderSecurityOperations(context.Background(), dir)
	require.NoError(t, err)
}

func withSealingCert(t *testing.T) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sealed-secrets-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certPath := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	orig := kubernetes.SealingCertSource
	kubernetes.SealingCertSource = certPath
	t.Cleanup(func() { kubernetes.SealingCertSource = orig })
}

// rendered is what the upgrade test compares, independent of file locations.
type rendered struct {
	// sealed: namespace/name of each SealedSecret -> its encryptedData.
	sealed map[string]string
	// credentials: kind/name:path of each credential leaf -> value.
	credentials map[string]string
	apps        map[string]bool
}

// credentialKeys are the rendered values that carry credential state.
var credentialKeys = map[string]bool{"passwordHash": true, "key": true, "redisPassword": true}

func scanRendered(t *testing.T, dir string) rendered {
	t.Helper()
	r := rendered{sealed: map[string]string{}, credentials: map[string]string{}, apps: map[string]bool{}}
	//nolint:gosec // G122/G703: dir is the test's own baseline or temporary render.
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".yaml") {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for i := 0; ; i++ {
			var doc map[string]any
			if err := dec.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return fmt.Errorf("%s: %w", rel, err)
			}
			kind, _ := doc["kind"].(string)
			meta, _ := doc["metadata"].(map[string]any)
			name, _ := meta["name"].(string)
			ns, _ := meta["namespace"].(string)
			var id string
			switch kind {
			case kindSealedSecret:
				spec, _ := doc["spec"].(map[string]any)
				out, _ := yaml.Marshal(spec["encryptedData"])
				r.sealed[ns+"/"+name] = string(out)
				continue
			case kindApplication:
				r.apps[name] = true
				id = kindApplication + "/" + name
			case "":
				// A values file: key by its base name.
				id = filepath.Base(rel)
			default:
				id = kind + "/" + name
			}
			collectCredentials(doc, id, r.credentials)
		}
	})
	require.NoError(t, err)
	return r
}

// sealedInstead reports the sealed Secret (namespace/name) that now holds a
// credential the previous release rendered in plain text, so that the upgrade
// keeps it without a rotation. Add a line here with such a move, never to
// silence a credential that simply changed.
func sealedInstead(id string) (string, bool) {
	switch {
	case strings.HasPrefix(id, kindApplication+"/wazuh-") &&
		strings.HasSuffix(id, ".helm.valuesObject.wazuh.wazuh.key"):
		// The Wazuh manager cluster key: wazuh-<code>/wazuh-manager-cluster-key.
		app := strings.TrimPrefix(strings.SplitN(id, ".", 2)[0], kindApplication+"/")
		return app + "/wazuh-manager-cluster-key", true
	case id == "values-security-operations.yaml.misp.misp.env.redisPassword":
		return constants.NamespaceSecurityOperations + "/" + constants.SecretNameMISPRedis, true
	}
	return "", false
}

func collectCredentials(v any, path string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if s, ok := child.(string); ok && credentialKeys[k] && s != "" {
				out[path+"."+k] = s
				continue
			}
			collectCredentials(child, path+"."+k, out)
		}
	case []any:
		for i, child := range t {
			collectCredentials(child, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

func stripIndexLines(diff string) string {
	var kept []string
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "index ") {
			continue
		}
		kept = append(kept, maskCiphertext(l))
	}
	return strings.Join(kept, "\n")
}

// ciphertextLine is one encryptedData entry of a SealedSecret.
var (
	ciphertextLine = regexp.MustCompile(`^([-+ ]\s+[A-Za-z0-9._-]+: )A[A-Za-z0-9+/=]{64,}$`)
	checksumLine   = regexp.MustCompile(`^([-+ ]# kubeaid-sha256: )[0-9a-f]{64}$`)
)

// maskCiphertext replaces a sealed value with a placeholder. Sealing is
// randomised, so a Secret this release adds would give a different golden file
// on every run; that a Secret the previous release sealed keeps its ciphertext
// is checked against before.sealed, not here.
func maskCiphertext(line string) string {
	line = ciphertextLine.ReplaceAllString(line, "${1}<sealed>")
	return checksumLine.ReplaceAllString(line, "${1}<sha256>")
}
