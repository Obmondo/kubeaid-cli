// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"go/ast"
	"go/token"
	"slices"
)

type Structs []*Struct

func NewStructsFromAST(ctx context.Context, imports map[string]string, node ast.Node) *Structs {
	structs := new(Structs)

	ast.Inspect(node, func(node ast.Node) bool {
		if _, ok := node.(*ast.File); ok {
			return true
		}

		declarationsNode, ok := node.(*ast.GenDecl)
		if !ok || (declarationsNode.Tok != token.TYPE) {
			return false
		}

		for _, declarationNode := range declarationsNode.Specs {
			typeDeclarationNode, ok := declarationNode.(*ast.TypeSpec)
			if !ok || (typeDeclarationNode.Type == nil) {
				return false
			}

			structDeclarationNode, ok := typeDeclarationNode.Type.(*ast.StructType)
			if !ok {
				return false
			}

			s := NewStructFromAST(ctx, imports, declarationsNode, typeDeclarationNode, structDeclarationNode)
			*structs = append(*structs, s)
		}

		return false
	})

	return structs
}

// For each struct, we remove the embedded struct fields, and add the corresponding promoted fields.
func (structs *Structs) ResolveEmbeddedStructFields() {
	for _, s := range *structs {
		for j, f := range s.Fields {
			if f.Embedded {
				promotedFields := structs.getFields(f.Name)
				s.Fields = append(s.Fields, promotedFields...)

				s.Fields = slices.Delete(s.Fields, j, j+1)
			}
		}
	}
}

// Returns fields for the given struct.
// We ignore embedded struct fields, considering the corresponding promoted fields.
func (structs *Structs) getFields(name string) []Field {
	fields := []Field{}

	for _, s := range *structs {
		/*
			We've found the struct we were looking for.
			Let's start collecting its fields.

			NOTE : I'm just using bruteforce approach, since we don't have any stress on the performance
			       requirements. Otherwise, this struct search operation can be optimized by sorting the
			       structs alphabetically and doing a binary search.
		*/
		if s.Name == name {
			for _, f := range s.Fields {
				switch f.Embedded {
				// For an embedded struct field, we collect the corresponding promoted fields.
				case true:
					promotedFields := structs.getFields(f.Name)
					fields = append(fields, promotedFields...)

				default:
					fields = append(fields, f)
				}
			}

			break
		}
	}

	return fields
}
