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

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
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
func installSealedSecretsWithFactory(ctx context.Context, factory HelmActionFactory) error {
	return helmInstallWithFactory(ctx, factory, sealedSecretsHelmArgs())
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
	if err := renameio.WriteFile(secretFilePath, sealedBytes, 0o600); err != nil {
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
// destinationFilePath — but only when the file's existing kubeaid-sha256
// header doesn't already match the hash of plaintextBytes. On a cache hit
// (existing header matches), leaves the file untouched: no kubeseal call,
// no rewrite, no git diff.
//
// On cache miss (header missing, header mismatched, or no file yet),
// runs kubeseal in memory, prepends the new hash header to the sealed
// bytes, and writes the result atomically in a single op via renameio —
// the plaintext never lands on disk and there's no transient half-
// written file the operator could trip over mid-bootstrap.
//
// The header is a YAML comment so the sealed-secrets-controller's
// reconciler doesn't see it; it's purely a kubeaid-cli-side cache key
// that happens to live inside the sealed-secret artifact file. Comment
// vs annotation tradeoff: comment doesn't propagate to the cluster-side
// Secret on decrypt, doesn't require post-process YAML manipulation,
// and is one prepended line vs ~30 LOC of structural edits.
func SealIfPlaintextChanged(ctx context.Context,
	destinationFilePath string,
	plaintextBytes []byte,
) error {
	newHash := sha256Hex(plaintextBytes)

	if existingHash, err := readKubeaidHashHeader(destinationFilePath); err == nil && existingHash == newHash {
		slog.InfoContext(ctx, "Sealed secret plaintext unchanged, skipping re-encryption",
			slog.String("path", destinationFilePath),
		)
		return nil
	}

	sealedBytes, err := sealPlaintextToBytes(ctx, plaintextBytes)
	if err != nil {
		return err
	}
	header := []byte(kubeaidHashHeaderPrefix + newHash + "\n")
	out := make([]byte, 0, len(header)+len(sealedBytes))
	out = append(out, header...)
	out = append(out, sealedBytes...)
	return renameio.WriteFile(destinationFilePath, out, 0o600)
}

// sealPlaintextToBytes runs kubeseal against plaintextBytes and returns
// the sealed-secret YAML bytes. Centralizes the kubeseal-client setup
// (load the controller's public key, build a sealing client config,
// call sealFn) so both GenerateSealedSecret and SealIfPlaintextChanged
// can produce sealed output without duplicating the wiring or paying
// the cost of a temp file on disk.
func sealPlaintextToBytes(ctx context.Context, plaintextBytes []byte) ([]byte, error) {
	kubesealClientConfig := newKubesealClientConfigFn()

	certReader, err := openCertFn(ctx, kubesealClientConfig,
		constants.NamespaceSealedSecrets, constants.SealedSecretsControllerName, "",
	)
	if err != nil {
		return nil, fmt.Errorf("reading sealed secrets controller's certificate: %w", err)
	}
	defer certReader.Close()

	publicKey, err := parseKeyFn(certReader)
	if err != nil {
		return nil, fmt.Errorf("retrieving sealed secrets controller's public key: %w", err)
	}

	var sealedBuf bytes.Buffer
	if err := sealFn(kubesealClientConfig,
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

// sha256Hex returns the lowercase-hex sha256 of b. Wraps the awkward
// sha256.Sum256 + hex.EncodeToString pair so call sites read linearly.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
