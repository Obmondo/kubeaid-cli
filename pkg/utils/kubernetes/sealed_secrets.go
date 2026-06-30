// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"

	sealedSecretsV1Aplha1 "github.com/bitnami-labs/sealed-secrets/pkg/apis/sealedsecrets/v1alpha1"
	"github.com/bitnami-labs/sealed-secrets/pkg/kubeseal"
	"github.com/google/renameio"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/logger"
)

var (
	newKubesealClientConfigFn = func() clientcmd.ClientConfig {
		return clientcmd.NewInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(), nil, os.Stdout,
		)
	}

	openCertFn = kubeseal.OpenCert
	parseKeyFn = kubeseal.ParseKey

	sealFn = func(clientConfig kubeseal.ClientConfig, outputFormat string,
		in io.Reader, out io.Writer,
		pubKey *rsa.PublicKey, scope sealedSecretsV1Aplha1.SealingScope,
	) error {
		return kubeseal.Seal(clientConfig,
			outputFormat,
			in,
			out,
			scheme.Codecs,
			pubKey,
			scope,
			false,
			"", "",
		)
	}

	renameTempFileFn = renameio.TempFile
)

// InstallSealedSecrets performs a minimal installation of Sealed Secrets in the underlying
// Kubernetes cluster. Honours the standard skip-if-deployed fast path —
// re-runs against a healthy install are cheap no-ops.
func InstallSealedSecrets(ctx context.Context) error {
	settings := cli.New()
	actionConfig := &action.Configuration{}
	err := actionConfig.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
		func(_ string, _ ...any) {},
	)
	if err != nil {
		return fmt.Errorf("failed initializing helm action config: %w", err)
	}

	if err := installSealedSecretsWithFactory(ctx, &realHelmFactory{cfg: actionConfig}); err != nil {
		return fmt.Errorf("failed installing sealed secrets helm chart: %w", err)
	}
	return nil
}

// ReinstallSealedSecrets is the recovery entry point used by
// EnsureSealedSecretsHealthy when the actual in-cluster state shows
// the controller's Deployment is missing or stuck. Uses `helm upgrade`
// — it works for releases in any non-pending state, reads the previous
// release manifest, re-renders the chart, and applies the diff against
// the live cluster — re-creating any drifted resources (e.g. an
// operator-manually-deleted Deployment).
//
// `helm install --replace` only handles uninstalled/failed states (per
// pkg/action/install.go::availableName), so it can't recover a
// release stuck in "deployed" with missing resources. Upgrade has no
// such restriction.
func ReinstallSealedSecrets(ctx context.Context) error {
	settings := cli.New()
	actionConfig := &action.Configuration{}
	err := actionConfig.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
		func(_ string, _ ...any) {},
	)
	if err != nil {
		return fmt.Errorf("failed initializing helm action config: %w", err)
	}

	if err := upgradeSealedSecretsWithFactory(ctx, &realHelmFactory{cfg: actionConfig}); err != nil {
		return fmt.Errorf("failed upgrading sealed secrets helm chart: %w", err)
	}
	return nil
}

// sealedSecretsHelmArgs centralises the chart path + namespace +
// values so install and upgrade entry points stay in sync.
func sealedSecretsHelmArgs() *HelmInstallArgs {
	return &HelmInstallArgs{
		ChartPath:   path.Join(utils.GetKubeAidDir(), "argocd-helm-charts/sealed-secrets"),
		Namespace:   constants.NamespaceSealedSecrets,
		ReleaseName: constants.ReleaseNameSealedSecrets,
		Values: &values.Options{
			Values: []string{
				fmt.Sprintf("sealed-secrets.namespace=%s", constants.NamespaceSealedSecrets),
				fmt.Sprintf(
					"sealed-secrets.fullnameOverride=%s", constants.SealedSecretsControllerName,
				),
				"backup=null",
			},
		},
	}
}

// installSealedSecretsWithFactory is the testable core of InstallSealedSecrets.
// A prior run can leave the release "failed" (install Wait timed out before the
// controller went Ready); recover such releases via helm upgrade.
func installSealedSecretsWithFactory(ctx context.Context, factory HelmActionFactory) error {
	args := sealedSecretsHelmArgs()
	err := helmInstallWithFactory(ctx, factory, args)

	var existsErr *ErrReleaseExistsNonDeployed
	if errors.As(err, &existsErr) && existsErr.RecoverableByUpgrade() {
		slog.WarnContext(ctx, "sealed-secrets release in a non-deployed state — recovering via helm upgrade",
			slog.String("status", string(existsErr.Status)))
		return helmUpgradeWithFactory(ctx, factory, args)
	}
	return err
}

// upgradeSealedSecretsWithFactory is the testable core of ReinstallSealedSecrets.
func upgradeSealedSecretsWithFactory(ctx context.Context, factory HelmActionFactory) error {
	return helmUpgradeWithFactory(ctx, factory, sealedSecretsHelmArgs())
}

// GenerateSealedSecret takes the path to a Kubernetes Secret file. It replaces the contents of
// that file by generating the corresponding Sealed Secret.
//
// Reads the plaintext from disk into a buffer, encrypts via the shared
// sealPlaintextToBytes helper, atomically writes the sealed YAML back
// in a single op. The buffer-and-write pattern means there's no
// transient half-written file on disk — either the original plaintext
// is there or the sealed output is, never both / neither.
func GenerateSealedSecret(ctx context.Context, secretFilePath string) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", secretFilePath),
	})

	plaintextBytes, err := os.ReadFile(secretFilePath)
	if err != nil {
		return fmt.Errorf("reading secret file: %w", err)
	}
	sealedBytes, err := sealPlaintextToBytes(ctx, plaintextBytes)
	if err != nil {
		return err
	}
	if err := writeSealedSecretFile(secretFilePath, sealedBytes); err != nil {
		return fmt.Errorf("atomically replacing secret file with sealed secret: %w", err)
	}
	return nil
}

// kubeaidHashHeaderPrefix is the leading-line marker we prepend to every
// kubeaid-cli-generated SealedSecret YAML. The value after the prefix is
// the sha256 hex of the rendered plaintext input. SealIfPlaintextChanged
// reads it on subsequent runs to short-circuit kubeseal regeneration when
// the plaintext hasn't changed — kubeseal uses a fresh AES key + nonce
// per Secret, so naive "always re-encrypt" produces git-diff noise on
// every re-run even when the underlying values are identical.
const kubeaidHashHeaderPrefix = "# kubeaid-sha256: "

// SealIfPlaintextChanged converts plaintextBytes to a SealedSecret YAML at
// destinationFilePath — but only when the cache header on the existing
// file doesn't already match the hash of (plaintextBytes ‖ controllerCert).
// On a cache hit, leaves the file untouched: no kubeseal call, no rewrite,
// no git diff.
//
// The cache key folds in the sealed-secrets controller's public cert so
// it invalidates on cluster re-key. Without that, recreating the
// management cluster (which provisions a fresh controller key) leaves
// every sealed-secret file's plaintext-hash matching but the cached
// ciphertext encrypted with the dead key — the new controller then
// fails to decrypt with "no key could decrypt secret" and the
// bootstrap dies later trying to use the empty Secret.
//
// On cache miss (header missing, header mismatched, or no file yet),
// runs kubeseal in memory using the just-loaded public key, prepends
// the new hash header to the sealed bytes, and writes atomically via
// renameio — the plaintext never lands on disk and there's no
// transient half-written file the operator could trip over mid-bootstrap.
//
// The header is a YAML comment so the sealed-secrets-controller's
// reconciler doesn't see it; it's purely a kubeaid-cli-side cache key
// that happens to live inside the sealed-secret artifact file.
func SealIfPlaintextChanged(ctx context.Context,
	destinationFilePath string,
	plaintextBytes []byte,
) error {
	certBytes, publicKey, err := loadSealingCert(ctx)
	if err != nil {
		return err
	}
	newHash := sha256Hex(plaintextBytes, certBytes)

	if existingHash, err := readKubeaidHashHeader(destinationFilePath); err == nil && existingHash == newHash {
		slog.InfoContext(ctx, "Sealed secret plaintext and controller cert unchanged, skipping re-encryption",
			slog.String("path", destinationFilePath),
		)
		return nil
	}

	sealedBytes, err := sealPlaintextWithKey(plaintextBytes, publicKey)
	if err != nil {
		return err
	}
	header := []byte(kubeaidHashHeaderPrefix + newHash + "\n")
	out := make([]byte, 0, len(header)+len(sealedBytes))
	out = append(out, header...)
	out = append(out, sealedBytes...)
	return writeSealedSecretFile(destinationFilePath, out)
}

func writeSealedSecretFile(filePath string, data []byte) error {
	pendingFile, err := renameTempFileFn("", filePath)
	if err != nil {
		return fmt.Errorf("creating temporary sealed-secret file: %w", err)
	}
	defer func() { _ = pendingFile.Cleanup() }()

	if _, err := pendingFile.Write(data); err != nil {
		return fmt.Errorf("writing temporary sealed-secret file: %w", err)
	}
	if err := pendingFile.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("replacing sealed-secret file: %w", err)
	}

	return nil
}

// sealPlaintextToBytes runs kubeseal against plaintextBytes and returns
// the sealed-secret YAML bytes. Convenience wrapper around
// loadSealingCert + sealPlaintextWithKey for callers (GenerateSealedSecret)
// that don't already have the cert loaded.
func sealPlaintextToBytes(ctx context.Context, plaintextBytes []byte) ([]byte, error) {
	_, publicKey, err := loadSealingCert(ctx)
	if err != nil {
		return nil, err
	}
	return sealPlaintextWithKey(plaintextBytes, publicKey)
}

// loadSealingCert reads the sealed-secrets controller's certificate from
// the cluster and returns both the raw cert bytes and the parsed RSA
// public key extracted from it.
//
// Returning both lets callers (SealIfPlaintextChanged) feed the cert
// bytes into the cache-key hash AND seal with the public key in a
// single API round-trip — the alternative is reading the cert twice
// (once for hashing, once for sealing), which doubles the bootstrap-time
// network calls per sealed secret.
func loadSealingCert(ctx context.Context) ([]byte, *rsa.PublicKey, error) {
	kubesealClientConfig := newKubesealClientConfigFn()
	certReader, err := openCertFn(ctx, kubesealClientConfig,
		constants.NamespaceSealedSecrets, constants.SealedSecretsControllerName, "",
	)
	if err != nil {
		return nil, nil, fmt.Errorf("reading sealed secrets controller's certificate: %w", err)
	}
	defer certReader.Close()

	certBytes, err := io.ReadAll(certReader)
	if err != nil {
		return nil, nil, fmt.Errorf("reading sealed secrets controller's certificate bytes: %w", err)
	}

	publicKey, err := parseKeyFn(bytes.NewReader(certBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("retrieving sealed secrets controller's public key: %w", err)
	}
	return certBytes, publicKey, nil
}

// sealPlaintextWithKey runs kubeseal against plaintextBytes using the
// caller-supplied public key. Split out from sealPlaintextToBytes so
// callers that already have the cert loaded don't fetch it twice.
func sealPlaintextWithKey(plaintextBytes []byte, publicKey *rsa.PublicKey) ([]byte, error) {
	var sealedBuf bytes.Buffer
	if err := sealFn(newKubesealClientConfigFn(),
		"yaml",
		bytes.NewReader(plaintextBytes),
		&sealedBuf,
		publicKey,
		sealedSecretsV1Aplha1.DefaultScope,
	); err != nil {
		return nil, fmt.Errorf("encrypting secret: %w", err)
	}
	return sealedBuf.Bytes(), nil
}

// sha256Hex returns the lowercase-hex sha256 of the concatenation of its
// arguments. Variadic so cache-key construction (plaintext ‖ cert) reads
// as one call instead of an explicit append dance at each call site.
func sha256Hex(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// readKubeaidHashHeader returns the hex hash from a "# kubeaid-sha256:
// <hex>" header on the first line of path, or "" when there is no such
// header (legacy file from before this code shipped, hand-edited file,
// or a corrupt header). Returns the underlying error only when the file
// itself can't be opened — a missing-or-malformed header is a cache miss,
// not a failure.
func readKubeaidHashHeader(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", scanner.Err()
	}
	line := scanner.Text()
	if !strings.HasPrefix(line, kubeaidHashHeaderPrefix) {
		return "", nil
	}
	return strings.TrimPrefix(line, kubeaidHashHeaderPrefix), nil
}
