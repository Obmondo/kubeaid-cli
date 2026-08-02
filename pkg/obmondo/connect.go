// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package obmondo fetches a cluster's configuration from the Obmondo portal.
//
// The portal's add-cluster flow collects every answer general.yaml and
// secrets.yaml need, then issues a short-lived single-use token. Passing that
// token to `cluster bootstrap --connect-obmondo` downloads the rendered files
// instead of running `config generate` — the portal has already done that job.
package obmondo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultAPIURL is the portal that issues tokens. Overridable so a
	// developer can point at a local or beta API.
	DefaultAPIURL = "https://api.obmondo.com/api"

	// installPath is the token-authenticated endpoint. It carries no JWT:
	// the token IS the authentication, and it is single-use.
	installPath = "/v1/cluster/install"

	// fetchTimeout bounds the whole exchange. The response is a handful of
	// small YAML documents, so a slow answer means something is wrong
	// rather than something being large.
	fetchTimeout = 30 * time.Second

	// obmondoDirName holds the mTLS material general.yaml points at. It
	// sits inside the configs directory so the whole cluster config is one
	// movable tree.
	obmondoDirName = "obmondo"

	certFileName = "cert.pem"
	keyFileName  = "key.pem"
	caFileName   = "ca.pem"
)

// PuppetMaterial is the mTLS identity kubeaid-agent authenticates to Obmondo
// with. It arrives inline and is written to disk here, because general.yaml
// references it by PATH (ObmondoConfig.CertPath / KeyPath) and only this
// process knows where the configs directory actually is.
type PuppetMaterial struct {
	Certname      string `json:"certname"`
	PrivateKey    string `json:"private_key"`
	Certificate   string `json:"certificate"`
	CACertificate string `json:"ca_certificate"`
}

// ClusterConfig is what the install endpoint returns.
type ClusterConfig struct {
	GeneralYAML string          `json:"general_yaml"`
	SecretsYAML string          `json:"secrets_yaml"`
	Puppet      *PuppetMaterial `json:"puppet"`
}

// apiResponse is the portal's standard envelope. Only the fields needed to
// find the payload or report a failure are declared.
type apiResponse struct {
	Success   bool            `json:"success"`
	Data      json.RawMessage `json:"data"`
	Message   string          `json:"message"`
	ErrorText string          `json:"error_text"`
}

// WrittenPaths reports where each file landed, for the command to print.
type WrittenPaths struct {
	General  string
	Secrets  string
	CertPath string
	KeyPath  string
	CAPath   string
}

// ClusterName reads cluster.name out of the fetched general.yaml.
//
// Needed before anything is written, because it names the directory the files
// go into. Read with a minimal struct rather than the full GeneralConfig so
// that a config carrying a field this binary predates still yields a name.
func ClusterName(generalYAML string) (string, error) {
	var probe struct {
		Cluster struct {
			Name string `yaml:"name"`
		} `yaml:"cluster"`
	}
	if err := yaml.Unmarshal([]byte(generalYAML), &probe); err != nil {
		return "", fmt.Errorf("reading the cluster name from general.yaml: %w", err)
	}
	if strings.TrimSpace(probe.Cluster.Name) == "" {
		return "", fmt.Errorf("general.yaml has no cluster.name")
	}
	return probe.Cluster.Name, nil
}

// DefaultConfigsDirectory is where a portal-fetched config lives when the
// operator has not chosen a location: <user config dir>/kubeaid-cli/<cluster>/configs,
// which is ~/.config/kubeaid-cli/<cluster>/configs on Linux.
//
// Deliberately NOT the flag's usual "outputs/configs" default, which is
// relative to the working directory. secrets.yaml holds cloud credentials and
// an mTLS private key, and an operator running this inside a checkout — the
// likely case, since they have a kubeaid-config repo — would leave those one
// `git add .` away from being committed. A path under the user's config
// directory cannot be committed by accident, and keeps each cluster's config
// separate without the operator having to think about directories at all.
func DefaultConfigsDirectory(clusterName string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(base, "kubeaid-cli", clusterName, "configs"), nil
}

// Fetch downloads the cluster configuration for token.
//
// The token is sent as a header rather than only in the query string: it
// keeps the secret out of any intermediary's request log, which routinely
// records URLs and rarely records headers. The query parameter is kept as
// well because the portal's endpoint reads it there, matching how the
// LinuxAid install link already works.
func Fetch(ctx context.Context, apiURL, token string) (*ClusterConfig, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("no Obmondo token supplied")
	}

	endpoint, err := url.Parse(strings.TrimRight(apiURL, "/") + installPath)
	if err != nil {
		return nil, fmt.Errorf("building Obmondo API URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("token", token)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building request to Obmondo: %w", err)
	}
	request.Header.Set("token", token)
	request.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: fetchTimeout}

	response, err := client.Do(request)
	if err != nil {
		// Deliberately not %w-wrapped with the URL: it carries the token.
		return nil, fmt.Errorf("could not reach the Obmondo API")
	}
	defer response.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("reading the Obmondo API response: %w", err)
	}

	switch response.StatusCode {
	case http.StatusOK:
		// fall through

	case http.StatusUnauthorized, http.StatusNotFound, http.StatusGone:
		// One message for every "this token will not work" case. The token
		// is single-use and short-lived, so an operator hitting any of
		// these needs the same next step: get a fresh one from the portal.
		return nil, fmt.Errorf(
			"this token is not valid — it may have expired or already been used; generate a new one from the Obmondo portal",
		)

	default:
		return nil, fmt.Errorf("the Obmondo API returned status %d", response.StatusCode)
	}

	var envelope apiResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("the Obmondo API returned something unreadable: %w", err)
	}
	if !envelope.Success {
		if envelope.ErrorText != "" {
			return nil, fmt.Errorf("the Obmondo API refused the request: %s", envelope.ErrorText)
		}
		return nil, fmt.Errorf("the Obmondo API refused the request")
	}

	config := new(ClusterConfig)
	if err := json.Unmarshal(envelope.Data, config); err != nil {
		return nil, fmt.Errorf("the Obmondo API returned an unexpected payload: %w", err)
	}
	if strings.TrimSpace(config.GeneralYAML) == "" {
		return nil, fmt.Errorf("the Obmondo API returned no general.yaml")
	}

	return config, nil
}

// Write lays the configuration out under configsDirectory and returns where
// each file landed.
//
// It refuses to overwrite an existing general.yaml. A token is single-use, so
// finding config already there means either a previous run succeeded — in
// which case bootstrap should simply be re-run without the flag — or there is
// unrelated config here that silently replacing would destroy.
func Write(configsDirectory string, config *ClusterConfig) (*WrittenPaths, error) {
	generalPath := filepath.Join(configsDirectory, "general.yaml")
	if _, err := os.Stat(generalPath); err == nil {
		return nil, fmt.Errorf(
			"%s already exists — re-run bootstrap without --connect-obmondo to use it",
			generalPath,
		)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("checking %s: %w", generalPath, err)
	}

	if err := os.MkdirAll(configsDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", configsDirectory, err)
	}

	written := &WrittenPaths{
		General: generalPath,
		Secrets: filepath.Join(configsDirectory, "secrets.yaml"),
	}

	// secrets.yaml and the private key are 0600 throughout: they carry the
	// customer's cloud credentials and mTLS identity.
	if err := os.WriteFile(written.Secrets, []byte(config.SecretsYAML), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", written.Secrets, err)
	}

	generalYAML := config.GeneralYAML

	if config.Puppet != nil {
		obmondoDir := filepath.Join(configsDirectory, obmondoDirName)
		if err := os.MkdirAll(obmondoDir, 0o700); err != nil {
			return nil, fmt.Errorf("creating %s: %w", obmondoDir, err)
		}

		written.CertPath = filepath.Join(obmondoDir, certFileName)
		written.KeyPath = filepath.Join(obmondoDir, keyFileName)
		written.CAPath = filepath.Join(obmondoDir, caFileName)

		files := []struct {
			path, contents string
			mode           os.FileMode
		}{
			{written.CertPath, config.Puppet.Certificate, 0o644},
			{written.KeyPath, config.Puppet.PrivateKey, 0o600},
			{written.CAPath, config.Puppet.CACertificate, 0o644},
		}
		for _, file := range files {
			if strings.TrimSpace(file.contents) == "" {
				continue
			}
			if err := os.WriteFile(file.path, []byte(file.contents), file.mode); err != nil {
				return nil, fmt.Errorf("writing %s: %w", file.path, err)
			}
		}

		// The portal cannot know these paths — they depend on
		// --configs-directory, which is a local choice — so it leaves
		// obmondo.certPath/keyPath empty and they are filled in here.
		generalYAML = withObmondoCertPaths(generalYAML, written.CertPath, written.KeyPath)
	}

	if err := os.WriteFile(written.General, []byte(generalYAML), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", written.General, err)
	}

	return written, nil
}

// withObmondoCertPaths fills in obmondo.certPath and obmondo.keyPath.
//
// Done as a line-level edit rather than a YAML round-trip on purpose: the
// portal's general.yaml is meant to be read by a human, and unmarshalling
// then re-marshalling would reorder keys, drop comments and reflow block
// scalars. Only the two values this process is authoritative about change.
func withObmondoCertPaths(generalYAML, certPath, keyPath string) string {
	replacements := map[string]string{
		"certPath": certPath,
		"keyPath":  keyPath,
	}

	lines := strings.Split(generalYAML, "\n")
	inObmondo := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track whether we are inside the top-level obmondo block. A new
		// top-level key (no leading whitespace, not a list item) ends it.
		if trimmed == "obmondo:" && !strings.HasPrefix(line, " ") {
			inObmondo = true
			continue
		}
		if inObmondo && line != "" && !strings.HasPrefix(line, " ") {
			inObmondo = false
		}
		if !inObmondo {
			continue
		}

		for key, value := range replacements {
			if strings.HasPrefix(trimmed, key+":") {
				indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
				lines[i] = fmt.Sprintf("%s%s: %q", indent, key, value)
			}
		}
	}

	return strings.Join(lines, "\n")
}

// LogPaths reports what was written, without echoing any contents.
func LogPaths(ctx context.Context, written *WrittenPaths) {
	attributes := []any{
		slog.String("general", written.General),
		slog.String("secrets", written.Secrets),
	}
	if written.CertPath != "" {
		attributes = append(attributes, slog.String("obmondoCert", written.CertPath))
	}
	slog.InfoContext(ctx, "Fetched cluster configuration from Obmondo", attributes...)
}
