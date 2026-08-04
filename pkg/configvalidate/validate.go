// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package configvalidate runs the struct-tag validation declared on
// pkg/config's schema.
//
// Kept out of pkg/config so that package stays at its own dependency
// weight: a caller that only needs the types pays nothing for
// go-playground/validator, and one that wants validation opts into ~20
// extra packages by importing this.
//
// Split out of pkg/config/parser so it can be reached without that
// package's parsing machinery — obmondo-api-go renders this config
// server-side and must be able to reject bad output before serving it,
// which previously meant importing a ~1180-package graph.
package configvalidate

import (
	"fmt"

	validatorV10 "github.com/go-playground/validator/v10"
	goNonStandardValidators "github.com/go-playground/validator/v10/non-standard/validators"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// StructTags validates generalConfig and secretsConfig against the
// validate: tags declared on them.
//
// This is schema validation only — it does not apply defaults, parse URLs,
// read SSH keys or reach the network, all of which pkg/config/parser does
// around it. Callers holding a freshly unmarshalled config should apply
// defaults first, or fields carrying both a default and notblank will fail.
func StructTags(generalConfig *config.GeneralConfig, secretsConfig *config.SecretsConfig) error {
	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	if err := validator.RegisterValidation("notblank", goNonStandardValidators.NotBlank); err != nil {
		return fmt.Errorf("failed registering notblank validator: %w", err)
	}
	if err := validator.Struct(generalConfig); err != nil {
		return fmt.Errorf("struct validation failed for general config: %w", err)
	}
	if err := validator.Struct(secretsConfig); err != nil {
		return fmt.Errorf("struct validation failed for secrets config: %w", err)
	}
	return nil
}
