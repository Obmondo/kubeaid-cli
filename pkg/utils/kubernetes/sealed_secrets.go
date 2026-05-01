// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"

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
// Kubernetes cluster.
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

// installSealedSecretsWithFactory is the testable core of InstallSealedSecrets.
func installSealedSecretsWithFactory(ctx context.Context, factory HelmActionFactory) error {
	return helmInstallWithFactory(ctx, factory, &HelmInstallArgs{
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
	})
}

// GenerateSealedSecret takes the path to a Kubernetes Secret file. It replaces the contents of
// that file by generating the corresponding Sealed Secret.
func GenerateSealedSecret(ctx context.Context, secretFilePath string) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", secretFilePath),
	})

	// Create Sealed Secrets controller client configuration.
	kubesealClientConfig := newKubesealClientConfigFn()

	// Load the Sealed Secrets controller's public key.

	certReader, err := openCertFn(ctx, kubesealClientConfig,
		constants.NamespaceSealedSecrets, constants.SealedSecretsControllerName, "",
	)
	if err != nil {
		return fmt.Errorf("failed reading sealed secrets controller's certificate: %w", err)
	}
	defer certReader.Close()

	publicKey, err := parseKeyFn(certReader)
	if err != nil {
		return fmt.Errorf("failed retrieving the sealed secrets controller's public key: %w", err)
	}

	// Open the file, from where KubeSeal will read the secret.
	secretFile, err := os.Open(secretFilePath)
	if err != nil {
		return fmt.Errorf("failed opening secret file: %w", err)
	}
	defer secretFile.Close()

	// Open the file, to where KubeSeal will write the sealed-secret.
	//
	// Notice, that it's the same file.
	// Behind the scenes, a temporary file is created, where kubeseal will write the Sealed Secret.
	// Contents of the Kubernetes Secret file will then be replaced with that of the temporary
	// Sealed Secret file.
	sealedSecretFile, err := renameTempFileFn("", secretFilePath)
	if err != nil {
		return fmt.Errorf("failed creating temporary sealed-secret file: %w", err)
	}

	// Encrypt the secret file.
	if err := sealFn(kubesealClientConfig,
		"yaml",
		secretFile,
		sealedSecretFile,
		publicKey,
		sealedSecretsV1Aplha1.DefaultScope,
	); err != nil {
		if cleanupErr := sealedSecretFile.Cleanup(); cleanupErr != nil {
			return fmt.Errorf("failed encrypting secret file: %w (cleanup also failed: %v)", err, cleanupErr)
		}
		return fmt.Errorf("failed encrypting secret file: %w", err)
	}

	if err := sealedSecretFile.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("failed atomically replacing secret file with sealed secret: %w", err)
	}

	return nil
}
