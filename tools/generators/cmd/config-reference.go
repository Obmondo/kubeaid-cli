// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"bytes"
	"context"
	_ "embed"
	"html/template"
	"os"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/tools/generators/pkg/structs"
)

const (
	TemplateNameConfigReference = "config-reference.md.tmpl"

	ConfigReferenceFilePath = "./docs/config-reference.md"
)

//go:embed templates/config-reference.md.tmpl
var configReferenceTemplate string

type ConfigReferenceTemplateValues struct {
	Structs *structs.Structs
}

func generateConfigReference(ctx context.Context, structs *structs.Structs) {
	// TODO : Sort the structs alphabetically by their name.

	// Execute the config reference markdown template.

	sproutFuncs := sprout.New(sprout.WithRegistries(
		strings.NewRegistry(),
	)).Build()

	parsedTemplate, err := template.New(TemplateNameConfigReference).
		Funcs(sproutFuncs).
		Parse(configReferenceTemplate)
	assert.AssertErrNil(ctx, err, "Failed parsing template")

	var executedTemplate bytes.Buffer
	err = parsedTemplate.Execute(&executedTemplate, ConfigReferenceTemplateValues{Structs: structs})
	assert.AssertErrNil(ctx, err, "Failed executing template")

	// Persist the KubeAid CLI config reference in the suitable markdown file.

	destinationFile, err := os.OpenFile(ConfigReferenceFilePath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	assert.AssertErrNil(ctx, err, "Failed opening file")
	defer destinationFile.Close()

	_, err = destinationFile.Write(executedTemplate.Bytes())
	assert.AssertErrNil(ctx, err, "Failed writing template execution result to file")
}
