// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	gostrings "strings"
	"text/template"

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

// typeLink returns a markdown-linked type string.
// If the base type name (stripped of "[]" prefix) exists in the known structs map,
// it renders as a markdown link pointing to the corresponding heading anchor.
// Otherwise, it returns the type name as-is.
func typeLink(knownStructs map[string]*structs.Struct, typeName string) string {
	prefix := ""
	base := typeName

	if gostrings.HasPrefix(base, "[]") {
		prefix = "[]"
		base = base[2:]
	}

	if _, ok := knownStructs[base]; ok {
		return fmt.Sprintf("%s[`%s`](#%s)", prefix, base, gostrings.ToLower(base))
	}

	return fmt.Sprintf("%s`%s`", prefix, base)
}

func generateConfigReference(ctx context.Context, structs *structs.Structs) {
	// Execute the config reference markdown template.

	sproutFuncs := sprout.New(sprout.WithRegistries(
		strings.NewRegistry(),
	)).Build()

	// Add custom template function for rendering type links.
	sproutFuncs["typeLink"] = func(typeName string) string {
		return typeLink(structs.All, typeName)
	}

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
