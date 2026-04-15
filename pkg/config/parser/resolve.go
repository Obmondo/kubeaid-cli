// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package parser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
)

// ResolveConfigsDirectory resolves the configs directory from a local path or stdin ("-").
// For stdin, it writes the received YAML to a temp directory and updates
// globals.ConfigsDirectory to point there.
func ResolveConfigsDirectory(ctx context.Context) error {
	if globals.ConfigsDirectory == "-" {
		return resolveFromStdin(ctx)
	}
	// Local path — nothing to resolve.
	return nil
}

// resolveFromStdin reads YAML from stdin and writes it as general.yaml to a temp directory.
// An empty secrets.yaml is also created so ParseConfigFiles doesn't fail.
func resolveFromStdin(ctx context.Context) error {
	slog.InfoContext(ctx, "Reading config from stdin")

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading from stdin: %w", err)
	}

	if len(data) == 0 {
		return fmt.Errorf("no data received from stdin")
	}

	tmpDir, err := os.MkdirTemp("", "kubeaid-configs-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	// Split into multiple YAML documents if both configs are provided in one stream.
	// First document = general.yaml, second document = secrets.yaml.
	docs, err := splitYAMLDocuments(data)
	if err != nil {
		return fmt.Errorf("parsing stdin YAML: %w", err)
	}
	if len(docs) == 0 {
		return fmt.Errorf("no YAML documents found on stdin")
	}

	if err := os.WriteFile(path.Join(tmpDir, "general.yaml"), docs[0], 0o600); err != nil {
		return fmt.Errorf("writing general.yaml: %w", err)
	}

	secretsContent := []byte("{}\n")
	if len(docs) > 1 {
		secretsContent = docs[1]
	}

	if err := os.WriteFile(path.Join(tmpDir, "secrets.yaml"), secretsContent, 0o600); err != nil {
		return fmt.Errorf("writing secrets.yaml: %w", err)
	}

	globals.ConfigsDirectory = tmpDir

	slog.InfoContext(ctx, "Config files written from stdin",
		slog.String("path", tmpDir),
	)

	return nil
}

// splitYAMLDocuments parses a multi-document YAML stream and returns each
// document re-encoded as its own byte slice. Empty documents (e.g. a stray
// leading "---") are skipped.
func splitYAMLDocuments(data []byte) ([][]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))

	var docs [][]byte
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding YAML document: %w", err)
		}

		// Skip empty documents — yaml.Node.Kind is zero for a document
		// with no content (e.g. a bare "---" separator).
		if node.Kind == 0 {
			continue
		}

		out, err := yaml.Marshal(&node)
		if err != nil {
			return nil, fmt.Errorf("re-encoding YAML document: %w", err)
		}
		docs = append(docs, out)
	}

	return docs, nil
}

// CleanupTempConfigsDirectory removes the temp directory if one was created during resolution.
// Safe to call even if configs were loaded from a local path.
func CleanupTempConfigsDirectory() {
	if !strings.HasPrefix(globals.ConfigsDirectory, os.TempDir()) {
		return
	}
	if !strings.Contains(globals.ConfigsDirectory, "kubeaid-configs-") {
		return
	}

	_ = os.RemoveAll(globals.ConfigsDirectory)
	globals.ConfigsDirectory = constants.FlagNameConfigsDirectoryDefaultValue
}
