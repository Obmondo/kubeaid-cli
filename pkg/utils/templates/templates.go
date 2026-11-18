// Copyright 2025 Obmondo
// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/encoding"
	"github.com/go-sprout/sprout/registry/strings"

	"github.com/Obmondo/kubeaid-cli/pkg/render"
)

// Stage is the step of rendering a template that failed.
type Stage string

const (
	StageReading   Stage = "reading"
	StageParsing   Stage = "parsing"
	StageExecuting Stage = "executing"
)

// Error is a failed rendering of the template called Template.
type Error struct {
	Stage    Stage
	Template string
	Err      error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s template %s: %v", e.Stage, e.Template, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// ParseAndExecuteTemplate executes the template stored at templateFileName in embeddedFS against
// values, with the sprout encoding and strings functions available. Trailing whitespace is
// stripped from the result. A failure is an *Error.
func ParseAndExecuteTemplate(
	embeddedFS *embed.FS,
	templateFileName string,
	values any,
) ([]byte, error) {
	contentsAsBytes, err := embeddedFS.ReadFile(templateFileName)
	if err != nil {
		return nil, &Error{Stage: StageReading, Template: templateFileName, Err: err}
	}

	sproutFuncs := sprout.New(sprout.WithRegistries(
		encoding.NewRegistry(),
		strings.NewRegistry(),
	)).Build()

	parsedTemplate, err := template.New(templateFileName).
		Funcs(sproutFuncs).
		Parse(string(contentsAsBytes))
	if err != nil {
		return nil, &Error{Stage: StageParsing, Template: templateFileName, Err: err}
	}

	var executedTemplate bytes.Buffer
	if err := parsedTemplate.Execute(&executedTemplate, values); err != nil {
		return nil, &Error{Stage: StageExecuting, Template: templateFileName, Err: err}
	}
	return render.StripTrailingWhitespace(executedTemplate.Bytes()), nil
}
