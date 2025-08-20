// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"

	sealedSecretsV1Aplha1 "github.com/bitnami-labs/sealed-secrets/pkg/apis/sealedsecrets/v1alpha1"
	"github.com/bitnami-labs/sealed-secrets/pkg/kubeseal"
	"github.com/google/renameio"
	"helm.sh/helm/v3/pkg/cli/values"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Performs a minimal installation of Sealed Secrets in the underlying Kubernetes cluster.
func InstallSealedSecrets(ctx context.Context) {
	HelmInstall(ctx, &HelmInstallArgs{
		ChartPath:   path.Join(constants.KubeAidDirectory, "argocd-helm-charts/sealed-secrets"),
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

// Takes the path to a Kubernetes Secret file. It replaces the contents of that file by generating
// the corresponding Sealed Secret.
func GenerateSealedSecret(ctx context.Context, secretFilePath string) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", secretFilePath),
	})

	// Create Sealed Secrets controller client configuration.
	kubesealClientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), nil, os.Stdout,
	)

	// Load the Sealed Secrets controller's public key.

	certReader, err := kubeseal.OpenCert(ctx, kubesealClientConfig,
		constants.NamespaceSealedSecrets, constants.SealedSecretsControllerName, "",
	)
	assert.AssertErrNil(ctx, err, "Failed reading Sealed Secrets controller's certificate")
	defer certReader.Close()

	publicKey, err := kubeseal.ParseKey(certReader)
	assert.AssertErrNil(ctx, err, "Failed retrieving the Sealed Secrets controller's public key")

	// Open the file, from where KubeSeal will read the secret.
	secretFile, err := os.Open(secretFilePath)
	assert.AssertErrNil(ctx, err, "Failed opening secret file")
	defer secretFile.Close()

	// Open the file, to where KubeSeal will write the sealed-secret.
	//
	// Notice, that it's the same file 👀.
	// Behind the scenes, a temporary file is created, where kubeseal will write the Sealed Secret.
	// Contents of the Kubernetes Secret file will then be replaced with that of the temporary
	// Sealed Secret file.
	sealedSecretFile, err := renameio.TempFile("", secretFilePath)
	assert.AssertErrNil(ctx, err, "Failed creating temporary sealed-secret file")
	defer func() {
		err := sealedSecretFile.CloseAtomicallyReplace()
		assert.AssertErrNil(ctx, err, "CloseAtomicReplace failed")
	}()

	// Encrypt the secret file.
	err = kubeseal.Seal(kubesealClientConfig,

		"yaml",
		secretFile,
		sealedSecretFile,

		scheme.Codecs,
		publicKey,
		sealedSecretsV1Aplha1.DefaultScope,
		false,

		"", "",
	)
	assert.AssertErrNil(ctx, err, "Failed encrypting secret file")
}
