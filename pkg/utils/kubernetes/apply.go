// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sAPIErrors "k8s.io/apimachinery/pkg/api/errors"
)

const fieldOwnerKubeaidCLI = "kubeaid-cli"

// ApplyManifestFromFile reads a (possibly multi-document) YAML file and
// applies each document to the cluster using server-side apply.
func ApplyManifestFromFile(ctx context.Context, clusterClient client.Client, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening manifest file %q: %w", filePath, err)
	}
	defer f.Close()

	return ApplyManifestFromReader(ctx, clusterClient, f)
}

// ApplyManifestFromURL fetches YAML from the given HTTP(S) URL and applies
// each document to the cluster using server-side apply.
func ApplyManifestFromURL(ctx context.Context, clusterClient client.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %q: %w", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching manifest from %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching manifest from %q: unexpected status %d", url, resp.StatusCode)
	}

	return ApplyManifestFromReader(ctx, clusterClient, resp.Body)
}

// ApplyManifestFromReader reads a multi-document YAML stream and applies each
// document to the cluster using server-side apply (patch with Apply strategy
// and force ownership).
func ApplyManifestFromReader(ctx context.Context, clusterClient client.Client, reader io.Reader) error {
	multidocReader := k8sYAML.NewYAMLReader(bufio.NewReader(reader))

	for {
		docBytes, err := multidocReader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("reading YAML document: %w", err)
		}

		// Skip empty documents (e.g. trailing "---").
		trimmed := strings.TrimSpace(string(docBytes))
		if trimmed == "" || trimmed == "---" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := k8sYAML.NewYAMLOrJSONDecoder(strings.NewReader(trimmed), len(docBytes)).Decode(obj); err != nil {
			return fmt.Errorf("decoding YAML document: %w", err)
		}

		// Server-side apply requires the object to have a valid GVK.
		if obj.GetKind() == "" {
			continue
		}

		err = clusterClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwnerKubeaidCLI))
		if err != nil {
			return fmt.Errorf("applying resource %s/%s (kind=%s): %w",
				obj.GetNamespace(), obj.GetName(), obj.GetKind(), err)
		}
	}

	return nil
}

// ReplaceForceFromDir reads all YAML files in the given directory and, for
// each document: deletes the existing resource (ignoring not-found) then
// creates it. This replicates `kubectl replace --force -f <dir>`.
func ReplaceForceFromDir(ctx context.Context, clusterClient client.Client, dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("reading directory %q: %w", dirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".json") {
			continue
		}

		filePath := filepath.Join(dirPath, name)
		if err := replaceForceFromFile(ctx, clusterClient, filePath); err != nil {
			return fmt.Errorf("replacing resources from file %q: %w", filePath, err)
		}
	}

	return nil
}

func replaceForceFromFile(ctx context.Context, clusterClient client.Client, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading file %q: %w", filePath, err)
	}

	multidocReader := k8sYAML.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		docBytes, err := multidocReader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("reading YAML document: %w", err)
		}

		trimmed := strings.TrimSpace(string(docBytes))
		if trimmed == "" || trimmed == "---" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := k8sYAML.NewYAMLOrJSONDecoder(strings.NewReader(trimmed), len(docBytes)).Decode(obj); err != nil {
			return fmt.Errorf("decoding YAML document: %w", err)
		}

		if obj.GetKind() == "" {
			continue
		}

		// Delete existing resource, ignoring not-found errors.
		existing := obj.DeepCopy()
		err = clusterClient.Get(ctx, client.ObjectKeyFromObject(existing), existing)
		if err == nil {
			if delErr := clusterClient.Delete(ctx, existing); delErr != nil && !k8sAPIErrors.IsNotFound(delErr) {
				return fmt.Errorf("deleting existing resource %s/%s (kind=%s): %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), delErr)
			}
		} else if !k8sAPIErrors.IsNotFound(err) {
			return fmt.Errorf("getting existing resource %s/%s (kind=%s): %w",
				obj.GetNamespace(), obj.GetName(), obj.GetKind(), err)
		}

		// Create the new resource.
		if err := clusterClient.Create(ctx, obj); err != nil {
			return fmt.Errorf("creating resource %s/%s (kind=%s): %w",
				obj.GetNamespace(), obj.GetName(), obj.GetKind(), err)
		}
	}

	return nil
}
