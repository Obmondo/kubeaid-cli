// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package obmondo fetches a cluster's configuration from the Obmondo portal.
//
// The portal's add-cluster flow collects every answer general.yaml and
// secrets.yaml need, then issues a short-lived bootstrap token paired with
// the cluster's certname. Passing both to `cluster bootstrap` downloads the
// rendered files instead of running `config generate` — the portal has
// already done that job. Passing the certname alone reuses whatever an
// earlier run already fetched for it.
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

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/cert"
	"github.com/Obmondo/kubeaid-cli/pkg/config/clusterdir"
	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

const (
	// DefaultAPIURL is the portal that issues tokens. Overridable so a
	// developer can point at a local or beta API.
	DefaultAPIURL = "https://api.obmondo.com/api"

	// installPath is the token-authenticated endpoint. It carries no JWT:
	// the token IS the authentication, paired with the certname it was
	// issued for. Redeeming does not consume it — it stays usable until it
	// expires, so a re-run needs no new link.
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

	generalFileName = "general.yaml"
	secretsFileName = "secrets.yaml"
)

// PuppetMaterial is the mTLS identity kubeaid-agent authenticates to Obmondo
// with. It arrives inline and is written to disk here, because general.yaml
// references it by PATH (ObmondoConfig.CertPath / KeyPath) and only this
// process knows where the configs directory actually is.
type PuppetMaterial struct {
	// Certname is overwritten with the Certificate's Subject CN on fetch:
	// the signed certificate is what bootstrap reads back to render into
	// cluster-vars.jsonnet, so the CN decides and this field only echoes it.
	// Independent of the cluster's name — the two are separate identifiers.
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

// Fetch downloads the cluster configuration for token.
//
// certname must be the one the token was issued for. It is checked
// server-side, so sending the wrong one is refused rather than silently
// serving another cluster's config — which is why it is required here and
// not derived from the response: the response is what the check protects.
//
// The token is sent as a header rather than only in the query string: it
// keeps the secret out of any intermediary's request log, which routinely
// records URLs and rarely records headers. The query parameter is kept as
// well because the portal's endpoint reads it there, matching how the
// LinuxAid install link already works.
func Fetch(ctx context.Context, apiURL, token, certname string) (*ClusterConfig, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("no Obmondo token supplied")
	}
	if strings.TrimSpace(certname) == "" {
		return nil, fmt.Errorf(
			"no certname supplied: pass --%s (or set %s) with the value the portal issued alongside the token",
			constants.FlagNameCertname, constants.EnvNameCertname,
		)
	}

	endpoint, err := url.Parse(strings.TrimRight(apiURL, "/") + installPath)
	if err != nil {
		return nil, fmt.Errorf("building Obmondo API URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("token", token)
	query.Set("certname", certname)
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
		// One message for every "this will not work" case. The API answers
		// an expired token and a certname that does not match the token
		// identically and on purpose, so there is nothing to tell apart
		// here — and either way the next step is the same.
		return nil, fmt.Errorf(
			"this token is not valid for %q — it may have expired, or been issued for a different cluster;"+
				" generate a new link from the Obmondo portal", certname,
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

	if err := adoptCertificateCN(config.Puppet); err != nil {
		return nil, err
	}

	return config, nil
}

// adoptCertificateCN checks the certificate is usable and takes its CN as the
// certname.
//
// The CN is the authoritative certname — bootstrap reads it back off this
// certificate (cert.ReadCN on ObmondoConfig.CertPath) and renders it into
// cluster-vars.jsonnet, so the JSON field is only an echo and the signed
// certificate is what actually decides. Deliberately NOT checked against
// cluster.name: a cluster's name and its certname are separate things, and
// tying them together here would reject a valid pairing the moment the portal
// stops deriving one from the other.
//
// Checked at fetch rather than at template time so an unusable certificate
// fails now, while the operator still has the terminal — discovering it
// during bootstrap would cost several minutes on a config that was never
// going to authenticate.
func adoptCertificateCN(material *PuppetMaterial) error {
	if material == nil || strings.TrimSpace(material.Certificate) == "" {
		return nil
	}

	cn, err := cert.CN([]byte(material.Certificate))
	if err != nil {
		return fmt.Errorf("the Obmondo API returned an unusable Puppet certificate: %w", err)
	}
	if cn == "" {
		return fmt.Errorf("the Obmondo API returned a Puppet certificate with no common name")
	}

	material.Certname = cn
	return nil
}

// pathsIn returns where each file a fetch writes lives under
// configsDirectory, whether or not any of them exist yet.
func pathsIn(configsDirectory string) *WrittenPaths {
	obmondoDir := filepath.Join(configsDirectory, obmondoDirName)
	return &WrittenPaths{
		General:  filepath.Join(configsDirectory, generalFileName),
		Secrets:  filepath.Join(configsDirectory, secretsFileName),
		CertPath: filepath.Join(obmondoDir, certFileName),
		KeyPath:  filepath.Join(obmondoDir, keyFileName),
		CAPath:   filepath.Join(obmondoDir, caFileName),
	}
}

// Complete reports whether every file a fetch writes is present under
// configsDirectory. All of them or none is the only useful answer: a
// half-written directory cannot bootstrap, and treating it as usable would
// fail much later with a much worse message.
func Complete(configsDirectory string) (*WrittenPaths, bool) {
	paths := pathsIn(configsDirectory)
	for _, path := range []string{paths.General, paths.Secrets, paths.CertPath, paths.KeyPath, paths.CAPath} {
		if _, err := os.Stat(path); err != nil {
			return paths, false
		}
	}
	return paths, true
}

// VerifyOnDisk accepts configsDirectory as this run's configuration, if it
// holds a complete file set whose Puppet certificate was issued for
// certname.
//
// The certificate's CN decides, never the directory's name: a cluster's name
// and its certname are separate identifiers (see adoptCertificateCN), so a
// path that looks right proves nothing about who the config authenticates
// as.
func VerifyOnDisk(configsDirectory, certname string) (*WrittenPaths, error) {
	paths, complete := Complete(configsDirectory)
	if !complete {
		return nil, fmt.Errorf(
			"%s does not hold a complete Obmondo configuration — pass --%s to fetch one",
			configsDirectory, constants.FlagNameToken,
		)
	}

	cn, err := cert.ReadCN(paths.CertPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", paths.CertPath, err)
	}
	if cn != certname {
		return nil, fmt.Errorf(
			"the certificate in %s was issued for %q, not %q",
			configsDirectory, cn, certname,
		)
	}

	return paths, nil
}

// FindOnDisk locates the configuration a previous run fetched for certname,
// among the per-cluster directories kubeaid-cli writes.
//
// Scans rather than deriving a path from certname, for the reason in
// VerifyOnDisk: the certificate's CN is the only reliable link between a
// certname and a directory named after a cluster.
func FindOnDisk(certname string) (string, *WrittenPaths, error) {
	clusters := clusterdir.List()

	for _, cluster := range clusters {
		directory, err := clusterdir.For(cluster)
		if err != nil {
			continue
		}
		paths, err := VerifyOnDisk(directory, certname)
		if err != nil {
			continue
		}
		return directory, paths, nil
	}

	if len(clusters) == 0 {
		return "", nil, fmt.Errorf(
			"no cluster configuration on this machine — pass --%s to fetch %s from Obmondo",
			constants.FlagNameToken, certname,
		)
	}
	return "", nil, fmt.Errorf(
		"nothing on this machine was issued for %s (found: %s) — pass --%s to fetch it",
		certname, strings.Join(clusters, ", "), constants.FlagNameToken,
	)
}

// Write lays the configuration out under configsDirectory and returns where
// each file landed.
//
// Existing files are overwritten. Redeeming a token is repeatable within its
// window, so a caller that got this far asked for this cluster's config by
// certname and got it — replacing what an earlier run wrote for the same
// certname is the intent, not an accident. Prompting instead would ask the
// same question on every re-run, which is the case most likely to be
// answered by reflex.
func Write(configsDirectory string, config *ClusterConfig) (*WrittenPaths, error) {
	if err := os.MkdirAll(configsDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", configsDirectory, err)
	}

	written := &WrittenPaths{
		General: filepath.Join(configsDirectory, generalFileName),
		Secrets: filepath.Join(configsDirectory, secretsFileName),
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

// LogReusedPaths reports the configuration an offline run adopted — the
// --certname-only path, where nothing was downloaded and nothing replaced.
func LogReusedPaths(ctx context.Context, written *WrittenPaths) {
	slog.InfoContext(ctx, "Using the cluster configuration already on disk",
		slog.String("general", written.General),
		slog.String("secrets", written.Secrets),
		slog.String("obmondoCert", written.CertPath),
	)
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

// PrintSecretsNotice tells the operator to back up the secrets this run
// wrote, and names where they landed.
//
// Printed rather than logged because stdout only receives ERROR records
// unless --debug is set: an slog line would go to the log file, which is
// not what the operator is reading when the run ends.
func PrintSecretsNotice(ctx context.Context, written *WrittenPaths) {
	if written == nil || written.Secrets == "" {
		return
	}

	// Pause the bar so its 100ms spinner auto-render can't \r-overwrite the
	// box mid-print — same fix as printHelpTextForArgoCDDashboardAccess.
	bar := progress.FromCtx(ctx)
	bar.Pause()
	defer bar.Resume()

	fmt.Println(renderSecretsNotice(written)) //nolint:forbidigo
}

// renderSecretsNotice lays the backup reminder out as a lipgloss bordered
// box, in the same style as the cluster-ready and PR-merge boxes the
// operator has already seen during this run. Amber header, matching the
// warning colour used elsewhere.
func renderSecretsNotice(written *WrittenPaths) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	pathStyle := lipgloss.NewStyle().Bold(true)
	noteStyle := lipgloss.NewStyle().Faint(true)

	lines := []string{
		headerStyle.Render("⚠ Back up secrets.yaml"),
		"",
		"  " + pathStyle.Render(written.Secrets),
	}

	// The private key is as sensitive as secrets.yaml and lands beside it.
	// Listed bare rather than annotated: an inline label would widen the box
	// past 80 columns, and obmondo/key.pem is self-describing here.
	if written.KeyPath != "" {
		lines = append(lines, "  "+pathStyle.Render(written.KeyPath))
	}

	// Phrased without claiming where these sit: the default path is outside
	// any checkout, but --configs-directory can put them anywhere, and
	// "nothing in git has a copy" would then be both wrong and reassuring.
	lines = append(lines,
		"",
		noteStyle.Render("Cloud credentials and cluster secrets. Keep out of git, and store"),
		noteStyle.Render("a copy somewhere safe — a pass repo or your password manager."),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
