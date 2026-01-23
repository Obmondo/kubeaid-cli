// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"maps"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-bootstrap-script/tools/generators/pkg/sourcefile"
	"github.com/Obmondo/kubeaid-bootstrap-script/tools/generators/pkg/structs"
)

func main() {
	ctx := context.Background()

	logger.InitLogger(false)

	assert.Assert(ctx,
		(len(os.Args) >= 3),
		"Usage : go run ./tools/config-reference-generate <source file path>....",
	)
	sourceFilePaths := os.Args[1:len(os.Args)]

	// Go through the source files,
	// gathering the structs.

	gatheredStructs := structs.Structs{
		All:   map[string]*structs.Struct{},
		Roots: []*structs.Struct{},
	}

	sourceFiles := make([]sourcefile.SourceFile, len(sourceFilePaths))
	for i, sourceFilePath := range sourceFilePaths {
		sourceFile := sourcefile.NewSourceFile(ctx, sourceFilePath)
		sourceFiles[i] = sourceFile

		maps.Copy(gatheredStructs.All, sourceFile.GetStructs().All)
		gatheredStructs.Roots = append(gatheredStructs.Roots, sourceFile.GetStructs().Roots...)
	}

	// Resolve embedded struct fields.
	gatheredStructs.ResolveEmbeddedStructFields()

	// Generate sample config files.
	(&SampleConfigGenerator{structs: &gatheredStructs}).generate(ctx)

	// Generate config reference markdown file.
	generateConfigReference(ctx, &gatheredStructs)
}
