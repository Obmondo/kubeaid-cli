// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"fmt"
	"os"
	"path"
	"text/template"

	"github.com/AlecAivazis/survey/v2"
)

// requiredInput prompts for a required text input field.
func requiredInput(message string, dest *string) error {
	return askOne(&survey.Input{Message: message}, dest, survey.WithValidator(survey.Required))
}

// requiredPassword prompts for a required password field (hidden input).
func requiredPassword(message string, dest *string) error {
	return askOne(&survey.Password{Message: message}, dest, survey.WithValidator(survey.Required))
}

// optionalInput prompts for an optional text input field with a default value.
func optionalInput(message string, defaultValue string, dest *string) error {
	return askOne(&survey.Input{Message: message, Default: defaultValue}, dest)
}

// confirm asks a yes/no question and stores the result.
func confirm(message string, defaultVal bool, dest *bool) error {
	return askOne(&survey.Confirm{Message: message, Default: defaultVal}, dest)
}

// selectOption presents a list of options for the user to choose from.
func selectOption(message string, options []string, defaultVal string, dest *string) error {
	return askOne(&survey.Select{Message: message, Options: options, Default: defaultVal}, dest)
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
