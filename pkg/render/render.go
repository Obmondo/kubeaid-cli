// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"text/template"
)

var (
	//go:embed templates/general.yaml.tmpl
	generalConfigTemplate string

	//go:embed templates/secrets.yaml.tmpl
	secretsConfigTemplate string
)

// Render produces general.yaml and secrets.yaml from cfg.
//
// Returns an error (never panics) on a nil cfg — unlike the CLI's own call
// sites, which always build cfg first, an external caller such as the
// Obmondo API can reach this with a malformed request, and Render runs
// inside a long-lived server process where a panic would take down more
// than the one request.
func Render(cfg *PromptedConfig) (general []byte, secrets []byte, err error) {
	if cfg == nil {
		return nil, nil, errors.New("cfg is nil")
	}

	general, err = renderTemplate("general.yaml", generalConfigTemplate, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("rendering general config: %w", err)
	}

	secrets, err = renderTemplate("secrets.yaml", secretsConfigTemplate, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("rendering secrets config: %w", err)
	}

	return general, secrets, nil
}

// renderTemplate executes a Go template string against data and returns the
// rendered bytes. name identifies the template in parse/execution error
// messages. Pure — no filesystem or network access.
func renderTemplate(name string, tmplStr string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", name, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return nil, fmt.Errorf("rendering template %s: %w", name, err)
	}

	return rendered.Bytes(), nil
}
