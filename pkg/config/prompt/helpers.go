// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/net/publicsuffix"
)

var (
	recapQuestionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7571F9")).Bold(true)
	recapAnswerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#02BF87"))
	recapMaskedAnswer  = "••••••••"
)

// deriveRealmFromDNS returns the first dot-separated segment of the
// effective TLD-plus-one for host. Returns "" when host has no public
// suffix or is otherwise unworkable — the parser's validator surfaces
// the error at parse time.
//
// Mirrors parser.deriveRealm; duplicated here to avoid an import cycle
// (parser imports config; prompt is upstream of both at config-write
// time).
func deriveRealmFromDNS(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	etldPlusOne, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}

	return strings.SplitN(etldPlusOne, ".", 2)[0]
}

// printRecap echoes the completed question and answer to stdout so the
// exchange persists after huh tears down its UI
func printRecap(question, answer string) {
	fmt.Printf("  %s %s\n", recapQuestionStyle.Render(question), recapAnswerStyle.Render(answer))
}

// errRequired is returned by the nonEmpty validator when the input is empty.
var errRequired = errors.New("value is required")

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errRequired
	}
	return nil
}

// httpsURL validates that s is a non-empty https:// URL with a host.
// Used for inputs where the protocol matters at bootstrap time
// (e.g. OIDC issuer URLs — kube-apiserver only fetches JWKS over
// TLS).
func httpsURL(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}

	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "https" {
		return errors.New("URL must start with https://")
	}

	if u.Host == "" {
		return errors.New("URL must include a host (https://<host>/...)")
	}

	return nil
}

// requiredHTTPSInput is requiredInput with httpsURL validation
// instead of nonEmpty.
func requiredHTTPSInput(message string, dest *string) error {
	if err := huh.NewInput().
		Title(message).
		Value(dest).
		Validate(httpsURL).
		Run(); err != nil {
		return err
	}
	printRecap(message, *dest)

	return nil
}

// requiredInput prompts for a required text input field.
func requiredInput(message string, dest *string) error {
	if err := huh.NewInput().
		Title(message).
		Value(dest).
		Validate(nonEmpty).
		Run(); err != nil {
		return err
	}
	printRecap(message, *dest)
	return nil
}

// requiredPassword prompts for a required password field (hidden input).
func requiredPassword(message string, dest *string) error {
	if err := huh.NewInput().
		Title(message).
		EchoMode(huh.EchoModePassword).
		Value(dest).
		Validate(nonEmpty).
		Run(); err != nil {
		return err
	}
	printRecap(message, recapMaskedAnswer)
	return nil
}

// optionalInput prompts for an optional text input field with a default value.
// The default is pre-filled so pressing Enter accepts it.
func optionalInput(message string, defaultValue string, dest *string) error {
	*dest = defaultValue
	if err := huh.NewInput().
		Title(message).
		Value(dest).
		Run(); err != nil {
		return err
	}
	printRecap(message, *dest)
	return nil
}

// confirm asks a yes/no question and stores the result.
// defaultVal pre-selects the default button.
func confirm(message string, defaultVal bool, dest *bool) error {
	*dest = defaultVal
	if err := huh.NewConfirm().
		Title(message).
		Value(dest).
		Run(); err != nil {
		return err
	}
	answer := "No"
	if *dest {
		answer = "Yes"
	}
	printRecap(message, answer)
	return nil
}

// selectOption presents a list of options for the user to choose from.
// defaultVal pre-selects the matching option (if present in options).
func selectOption(message string, options []string, defaultVal string, dest *string) error {
	opts := make([]huh.Option[string], 0, len(options))
	for _, o := range options {
		opts = append(opts, huh.NewOption(o, o))
	}
	if defaultVal != "" {
		*dest = defaultVal
	}
	if err := huh.NewSelect[string]().
		Title(message).
		Options(opts...).
		Value(dest).
		Run(); err != nil {
		return err
	}
	printRecap(message, *dest)
	return nil
}

// writeTemplatedFile renders a Go template string with the given data and writes it to disk.
func writeTemplatedFile(filePath string, tmplStr string, data any, perm os.FileMode) error {
	dir := path.Dir(filePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", filePath, err)
	}
	defer f.Close()

	tmpl, err := template.New(path.Base(filePath)).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", filePath, err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("rendering template %s: %w", filePath, err)
	}

	return nil
}
