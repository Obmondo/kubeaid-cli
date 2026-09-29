// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package root

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/secrets"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/velociraptor"
)

var publishFlags struct {
	file      string
	namespace string
	name      string
	key       string
}

// publishCmd stores a freshly minted Velociraptor api_client config in
// a Secret. It runs as an initContainer next to the Velociraptor
// server, after `velociraptor config api_client` wrote the file.
var publishCmd = &cobra.Command{
	Use:   "publish-api-client",
	Short: "Store a Velociraptor api_client config file in a Kubernetes Secret",
	Long: `publish-api-client validates an api_client YAML (from "velociraptor config api_client")
and writes it into a Secret key, creating the Secret if needed and replacing the key when
the content changed. This command writes; it has no dry-run mode.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if publishFlags.namespace == "" {
			publishFlags.namespace = os.Getenv("POD_NAMESPACE")
		}
		if publishFlags.file == "" || publishFlags.namespace == "" {
			return errors.New("--file and --namespace (or POD_NAMESPACE) are required")
		}
		data, err := os.ReadFile(publishFlags.file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", publishFlags.file, err)
		}
		if _, err := velociraptor.ParseAPIClient(data); err != nil {
			return err
		}
		kube, err := kubeClient()
		if err != nil {
			return err
		}
		action, err := secrets.Publish(cmd.Context(), kube, publishFlags.namespace, publishFlags.name, publishFlags.key, data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "secret %s/%s key %s: %s\n", publishFlags.namespace, publishFlags.name, publishFlags.key, action)
		return err
	},
}

func init() {
	f := publishCmd.Flags()
	f.StringVar(&publishFlags.file, "file", "", "api_client YAML to publish")
	f.StringVar(&publishFlags.namespace, "namespace", "", "Secret namespace (default $POD_NAMESPACE)")
	f.StringVar(&publishFlags.name, "name", "velociraptor-api-client", "Secret name")
	f.StringVar(&publishFlags.key, "key", "api_client.yaml", "Secret key")
}
