// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package structs

import (
	"context"
	"go/ast"
	"log/slog"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type Struct struct {
	Name,
	Doc string

	Fields []Field
}

func NewStructFromAST(ctx context.Context,
	imports map[string]string,
	declarationsNode *ast.GenDecl,
	typeDeclarationNode *ast.TypeSpec,
	structDeclarationNode *ast.StructType,
) *Struct {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("struct", typeDeclarationNode.Name.Name),
	})

	/*
		Get the doc comment. There can be 2 scenarios :

		  (1) The doc comment is attached to the type declaration node. An example :

		    type (
		      // Non secret configuration options.
		      GeneralConfig struct {
		        // KubeAid and KubeAid Config repository speicific details.
		        // For now, we require the KubeAid and KubeAid Config repositories to be hosted in the
		        // same Git server.
		        Forks ForksConfig `yaml:"forkURLs" validate:"required"`
		      }
		    )

		  (2) The doc comment is attached to the declarations node. An example :

		    // Non secret configuration options.
		    type GeneralConfig struct {
		      // KubeAid and KubeAid Config repository speicific details.
		      // For now, we require the KubeAid and KubeAid Config repositories to be hosted in the
		      // same Git server.
		      Forks ForksConfig `yaml:"forkURLs" validate:"required"`
		    }

		Otherwise, the struct doesn't have any doc comment. And we'll error out, complaining about that.
	*/
	var doc string
	switch {
	case typeDeclarationNode.Doc != nil:
		doc = typeDeclarationNode.Doc.Text()

	case (declarationsNode.Doc != nil) && (len(declarationsNode.Specs) == 1):
		doc = declarationsNode.Doc.Text()

	default:
		// It doesn't make sense to have doc comments for structs like NodeGroup or
		// AutoScalableNodeGroup.
		// So, if doc comment isn't found for a struct, we just warn, and not error out.
		// TODO : uncomment.
		// slog.WarnContext(ctx, "No doc comment found")
	}
	doc = "<p>" + strings.TrimSpace(doc) + "</p>"

	// Get the fields.
	// For now, we'll have embedded struct fields, if any. Later, we'll remove those embedded struct
	// fields, and add corresponding promoted fields.

	fields := []Field{}

	for _, node := range structDeclarationNode.Fields.List {
		// Field isn't settable by user, since it doesn't have the yaml struct tag.
		// So, we'll skip it.
		if (node.Tag == nil) || (len(getStructTags(node).Get("yaml")) == 0) {
			continue
		}

		fields = append(fields,
			NewFieldFromAST(ctx, imports, node))
	}

	return &Struct{
		Name:   typeDeclarationNode.Name.Name,
		Doc:    doc,
		Fields: fields,
	}
}
