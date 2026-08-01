// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-cli/pkg/config/schema"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-cli/tools/generators/pkg/structs"
)

// FormSpecFilePath is where the emitted JSON is written, colocated with
// the package that embeds it — see pkg/config/schema.FormSpec.
const FormSpecFilePath = "./pkg/config/schema/formspec.json"

// whenCondition is a field's resolved appliesWhen — inherited down the
// struct tree from the nearest ancestor field that declared a `when`
// struct tag, unless a field declares its own.
type whenCondition struct {
	path   string
	values []string
}

func (w whenCondition) appliesWhen() map[string][]string {
	if w.path == "" {
		return nil
	}
	return map[string][]string{w.path: w.values}
}

// generateFormSpec builds the add-cluster web form's schema and writes it
// as indented JSON to FormSpecFilePath.
func generateFormSpec(ctx context.Context, allStructs *structs.Structs) {
	encoded, err := json.MarshalIndent(buildFormSpec(allStructs), "", "  ")
	assert.AssertErrNil(ctx, err, "Failed marshaling form spec")

	destinationFile, err := os.OpenFile(FormSpecFilePath,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	assert.AssertErrNil(ctx, err, "Failed opening form spec file")
	defer destinationFile.Close()

	_, err = destinationFile.Write(append(encoded, '\n'))
	assert.AssertErrNil(ctx, err, "Failed writing form spec file")
}

// buildFormSpec turns allStructs' two root structs (GeneralConfig,
// SecretsConfig) into a schema.Spec, one section per root-level field.
// Pure — no filesystem access — so it is unit-testable on its own.
func buildFormSpec(allStructs *structs.Structs) schema.Spec {
	var sections []schema.Section
	for _, root := range allStructs.Roots {
		for _, field := range root.Fields {
			sections = append(sections, formSpecSection(allStructs, field))
		}
	}

	return schema.Spec{Sections: sections}
}

// formSpecSection turns one root struct's own top-level field (e.g.
// GeneralConfig.Cluster) into a section: its yaml key is the section key,
// and its nested struct's fields — flattened, with paths prefixed by that
// key — are the section's fields.
func formSpecSection(allStructs *structs.Structs, field structs.Field) schema.Section {
	section := schema.Section{
		Key:   field.Name,
		Title: sectionTitle(field),
	}

	path := []string{field.Name}
	when := whenCondition{path: field.WhenPath, values: field.WhenValues}

	if childStruct, ok := allStructs.All[field.Type]; ok {
		section.Fields = visitFields(allStructs, childStruct, path, when)
		return section
	}

	// No root field is a non-struct today (every GeneralConfig and
	// SecretsConfig top-level field is itself a config struct), but a
	// bare value here is handled rather than assumed away.
	section.Fields = []schema.Field{toSchemaField(field, path, when)}
	return section
}

// visitFields recursively flattens s's fields into schema.Field leaves.
// Each leaf's Path is prefixed with pathPrefix; each leaf's AppliesWhen
// is inheritedWhen unless the field (or an ancestor closer to it)
// declared its own `when` tag. A field whose Go type resolves to another
// known struct is descended into rather than emitted as a leaf — this is
// also what makes a slice-of-struct field (whose Type string, e.g.
// "[]HCloudAutoScalableNodeGroup", never matches a bare struct name)
// terminate as a single leaf instead of being flattened, since a repeating
// group has no single fixed path to flatten its element fields under.
func visitFields(allStructs *structs.Structs, s *structs.Struct, pathPrefix []string, inheritedWhen whenCondition) []schema.Field {
	var fields []schema.Field

	for _, field := range s.Fields {
		path := make([]string, 0, len(pathPrefix)+1)
		path = append(path, pathPrefix...)
		path = append(path, field.Name)

		when := inheritedWhen
		if field.WhenPath != "" {
			when = whenCondition{path: field.WhenPath, values: field.WhenValues}
		}

		if childStruct, ok := allStructs.All[field.Type]; ok {
			fields = append(fields, visitFields(allStructs, childStruct, path, when)...)
			continue
		}

		fields = append(fields, toSchemaField(field, path, when))
	}

	return fields
}

func toSchemaField(field structs.Field, path []string, when whenCondition) schema.Field {
	return schema.Field{
		Path:        path,
		Type:        field.Type,
		Tier:        field.Tier,
		Required:    field.Required,
		Default:     field.DefaultValue,
		Enum:        field.EnumOptions,
		AppliesWhen: when.appliesWhen(),
		Doc:         strings.TrimSpace(field.Doc),
	}
}

// sectionTitle derives a human title for a root struct's top-level
// field: its doc comment up to the first period or line break —
// whichever comes first, so a wrapped first sentence is cut at the
// source line break rather than spilling the rest of the paragraph into
// a section header — or the capitalized yaml key when there's no doc
// comment at all.
func sectionTitle(field structs.Field) string {
	doc := strings.TrimSpace(field.Doc)
	if doc == "" {
		if field.Name == "" {
			return field.Name
		}
		return strings.ToUpper(field.Name[:1]) + field.Name[1:]
	}

	end := len(doc)
	if i := strings.IndexAny(doc, ".\n"); i >= 0 {
		end = i
	}
	return strings.TrimSpace(doc[:end])
}
