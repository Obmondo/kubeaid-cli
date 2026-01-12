// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type SourceFile struct {
	// For each package import, we map the import name to the import path.
	Imports map[string]string

	Structs *Structs
}

func NewSourceFile(ctx context.Context, path string) SourceFile {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", path),
	})

	// Determine the absolute path.
	path, err := filepath.Abs(path)
	assert.AssertErrNil(ctx, err, "Failed determining absolute path")

	// Parse the sourcecode.
	node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	assert.AssertErrNil(ctx, err, "Failed parsing source file")

	// For each package import, map the import name to import path.
	// We'll need this mapping later.
	imports := make(map[string]string)
	for _, i := range node.Imports {
		importPath := strings.Trim(i.Path.Value, "\"")

		switch i.Name {
		case nil:
			parts := strings.Split(importPath, "/")
			imports[parts[len(parts)-1]] = importPath

		// Named import.
		default:
			imports[i.Name.Name] = importPath
		}
	}

	// Collect structs.
	structs := NewStructsFromAST(ctx, imports, node)

	return SourceFile{
		Imports: imports,
		Structs: structs,
	}
}
