// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/Obmondo/kubeaid-bootstrap-script/tools/generators/pkg/structs"
)

const (
	SampleGeneralConfigFilePath = "./cmd/kubeaid-core/root/config/generate/general.yaml"
	SampleSecretsConfigFilePath = "./cmd/kubeaid-core/root/config/generate/secrets.yaml"
)

type SampleConfigGenerator struct {
	structs *structs.Structs
}

func (s *SampleConfigGenerator) generate(ctx context.Context) {
	for _, rootStruct := range s.structs.Roots {
		// Determine the sample config file path,
		// based on which root struct it is.

		var sampleConfigFilePath string

		switch rootStruct.Name {
		case structs.RootStructNameGeneralConfig:
			sampleConfigFilePath = SampleGeneralConfigFilePath

		case structs.RootStructNameSecretsConfig:
			sampleConfigFilePath = SampleSecretsConfigFilePath

		default:
			slog.Error("Unknown root struct", slog.String("name", rootStruct.Name))
			os.Exit(1)
		}

		scopedCtx := logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
			slog.String("file", sampleConfigFilePath),
		})

		// Open the sample config file.
		sampleConfigFile, err := os.OpenFile(sampleConfigFilePath,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			0o600,
		)
		assert.AssertErrNil(scopedCtx, err, "Failed opening sample config file")
		defer sampleConfigFile.Close()

		// Starting from the root struct, we keep going down,
		// until we hit fields with primitive types.
		s.visitStruct(scopedCtx, sampleConfigFile, rootStruct, "")
	}
}

func (s *SampleConfigGenerator) visitStruct(ctx context.Context,
	w io.Writer,
	_struct *structs.Struct,
	indentation string,
) {
	for _, field := range _struct.Fields {
		if len(field.Doc) > 0 {
			for line := range strings.SplitSeq(field.Doc, "\n") {
				_, err := fmt.Fprintf(w, "%s# %s\n", indentation, line)
				assert.AssertErrNil(ctx, err, "Failed writing to file")
			}
		}

		_, err := fmt.Fprintf(w, "%s%s:\n\n", indentation, field.Name)
		assert.AssertErrNil(ctx, err, "Failed writing to file")

		childStruct, ok := s.structs.All[field.Type]
		if !ok {
			continue
		}

		s.visitStruct(ctx, w, childStruct, indentation+"  ")
	}
}
