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
	"slices"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
)

// TierBasic is the only recognized value of the `tier` struct tag. Absent
// (empty Field.Tier) means advanced — see NewFieldFromAST.
const TierBasic = "basic"

type Field struct {
	Name,
	Type,
	Doc string

	Embedded bool

	DefaultValue string

	// Required and EnumOptions are derived from the `validate` struct tag:
	// Required from a top-level `required` or `notblank` rule (false when
	// `omitempty` is present), EnumOptions from a top-level `oneof=a b c`
	// rule. Rules after a `dive` govern slice/map elements, not the field
	// itself, and are ignored by both.
	Required    bool
	EnumOptions []string

	// Tier is "basic" (shown to a first-time user by default) or "" —
	// advanced, collapsed behind a UI disclosure. From the `tier` struct tag.
	Tier string

	// WhenPath / WhenValues are derived from the `when` struct tag —
	// `when:"cloud.hetzner.mode=hcloud|hybrid"` sets WhenPath to
	// "cloud.hetzner.mode" and WhenValues to ["hcloud","hybrid"]. WhenPath
	// is "" when the field always applies.
	WhenPath   string
	WhenValues []string
}

func NewFieldFromAST(ctx context.Context, imports map[string]string, node *ast.Field) Field {
	var (
		structTags = getStructTags(node)

		yamlStructTag     = structTags.Get("yaml")
		defaultStructTag  = structTags.Get("default")
		validateStructTag = structTags.Get("validate")
		tierStructTag     = structTags.Get("tier")
		whenStructTag     = structTags.Get("when")
	)

	assert.Assert(ctx,
		(tierStructTag == "") || (tierStructTag == TierBasic),
		"tier struct tag must be either absent or \"basic\"",
		slog.String("tier", tierStructTag),
	)

	required, enumOptions := parseValidateStructTag(validateStructTag)
	whenPath, whenValues := parseWhenStructTag(ctx, whenStructTag)

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
			Required:     required,
			EnumOptions:  enumOptions,
			Tier:         tierStructTag,
			WhenPath:     whenPath,
			WhenValues:   whenValues,
		}
	}
}

// parseValidateStructTag extracts the field-level required flag and
// `oneof=` enum options from a `validate:"..."` struct tag. Only the forms
// present in pkg/config/general.go and pkg/config/secrets.go are handled:
// notblank, required, omitempty, oneof=a b c, and dive plus whatever
// element-level rules follow it.
//
// Rules after "dive" apply to slice/map elements, not the field itself
// (e.g. `omitempty,dive,required` means the field may be empty, but each
// element of a non-empty slice is required) — they never contribute to the
// field's own Required or EnumOptions.
func parseValidateStructTag(tag string) (required bool, enumOptions []string) {
	if tag == "" {
		return false, nil
	}

	rules := strings.Split(tag, ",")
	if i := slices.Index(rules, "dive"); i >= 0 {
		rules = rules[:i]
	}

	omitempty := false
	for _, rule := range rules {
		switch {
		case rule == "omitempty":
			omitempty = true
		case (rule == "required") || (rule == "notblank"):
			required = true
		case strings.HasPrefix(rule, "oneof="):
			enumOptions = strings.Fields(strings.TrimPrefix(rule, "oneof="))
		}
	}
	if omitempty {
		required = false
	}

	return required, enumOptions
}

// parseWhenStructTag parses a `when:"<dotted schema path>=<value>[|<value>...]"`
// struct tag — e.g. `when:"cloud.hetzner.mode=hcloud|hybrid"`. Returns
// ("", nil) when tag is empty, meaning the field always applies.
func parseWhenStructTag(ctx context.Context, tag string) (path string, values []string) {
	if tag == "" {
		return "", nil
	}

	path, rawValues, ok := strings.Cut(tag, "=")
	assert.Assert(ctx, ok,
		"when struct tag must be \"<dotted path>=<value>[|<value>...]\"",
		slog.String("when", tag),
	)

	return path, strings.Split(rawValues, "|")
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
