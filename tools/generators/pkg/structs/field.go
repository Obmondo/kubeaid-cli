// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package structs

import (
	"context"
	"fmt"
	"go/ast"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type Field struct {
	Name,
	Type,
	Doc string

	Embedded bool

	DefaultValue string
}

func NewFieldFromAST(ctx context.Context, imports map[string]string, node *ast.Field) Field {
	var (
		structTags = getStructTags(node)

		yamlStructTag    = structTags.Get("yaml")
		defaultStructTag = structTags.Get("default")
	)

	t := getFieldTypeAsString(ctx, imports, node.Type)

	switch node.Names {
	// Embedded struct field.
	case nil:
		assert.Assert(ctx,
			(yamlStructTag == ",inline"),
			"Expected nameless field to be an embedded struct",
		)

		return Field{
			Name:     t,
			Type:     t,
			Embedded: true,
		}

	default:
		// We require every field to have a doc comment.
		// assert.AssertNotNil(ctx, node.Doc, "No doc comment found", slog.String("field", name))
		// doc := strings.ReplaceAll(node.Doc.Text(), "\n", "<br>")

		return Field{
			Name:         yamlStructTag,
			Type:         t,
			Doc:          node.Doc.Text(),
			DefaultValue: defaultStructTag,
		}
	}
}

// Returns struct tags for the given struct field AST node.
func getStructTags(node *ast.Field) reflect.StructTag {
	return reflect.StructTag(strings.Trim(node.Tag.Value, "`"))
}

// Returns the struct field type as string.
// Note that, import names are expanded to import paths.
func getFieldTypeAsString(ctx context.Context, imports map[string]string, node any) string {
	switch node := node.(type) {
	case *ast.Ident:
		return node.Name

	case *ast.StarExpr:
		return getFieldTypeAsString(ctx, imports, node.X)

	case *ast.ArrayType:
		return fmt.Sprintf("[]%s", getFieldTypeAsString(ctx, imports, node.Elt))

	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s",
			getFieldTypeAsString(ctx, imports, node.Key), getFieldTypeAsString(ctx, imports, node.Value))

	case *ast.SelectorExpr:
		identifier, _ := node.X.(*ast.Ident)
		importPath := imports[identifier.Name]
		return fmt.Sprintf("%s.%s", importPath, node.Sel.Name)

	default:
		slog.ErrorContext(ctx, "Unsupported struct field type", slog.Any("node", node))
		os.Exit(1)
	}

	return ""
}
