// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	recapQuestionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7571F9")).Bold(true)
	recapAnswerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#02BF87"))
	recapMaskedAnswer  = "••••••••"
)

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
