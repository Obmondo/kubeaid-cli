// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"bytes"
	"context"
	"os"
	"sort"
	"text/template"

	_ "embed"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

const TemplateNameConfigReference = "config-reference.md.tmpl"

//go:embed config-reference.md.tmpl
var configReferenceTemplate string

type ConfigReferenceTemplateValues struct {
	Structs *Structs
}

func main() {
	ctx := context.Background()

	logger.InitLogger(false)

	assert.Assert(
		ctx,
		(len(os.Args) >= 3),
		"Usage : go tool github.com/Obmondo/kubeaid-bootstrap-script/tools/config-reference-generate <source 1> <source 2>.... <output>",
	)
	var (
		sourceFilePaths         = os.Args[1:(len(os.Args) - 1)]
		configReferenceFilePath = os.Args[(len(os.Args) - 1)]
	)

	// Get the KubeAid CLI config reference.
	configReference := generateConfigReference(ctx, sourceFilePaths)

	// Persist the KubeAid CLI config reference in a markdown file.

	destinationFile, err := os.OpenFile(configReferenceFilePath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	assert.AssertErrNil(ctx, err, "Failed opening file")
	defer destinationFile.Close()

	_, err = destinationFile.Write(configReference)
	assert.AssertErrNil(ctx, err, "Failed writing template execution result to file")
}

// Returns the KubeAid CLI config reference, by reading the given source files.
func generateConfigReference(ctx context.Context, sourceFilePaths []string) []byte {
	sourceFiles := make([]SourceFile, len(sourceFilePaths))
	for i, sourceFilePath := range sourceFilePaths {
		sourceFiles[i] = NewSourceFile(ctx, sourceFilePath)
	}

	// Gather all the structs in a single slice.
	structs := new(Structs)
	for _, sourceFile := range sourceFiles {
		*structs = append(*structs, *sourceFile.Structs...)
	}

	// Sort the structs alphabetically by their name.
	sort.Slice(*structs, func(i int, j int) bool {
		return (*structs)[i].Name < (*structs)[j].Name
	})

	// Resolve embedded struct fields.
	structs.ResolveEmbeddedStructFields()

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

	return executedTemplate.Bytes()
}
