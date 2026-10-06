// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyLicense(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "Apache", content: "Apache License\nVersion 2.0", want: "Apache-2.0"},
		{name: "MPL", content: "Mozilla Public License, v. 2.0", want: "MPL-2.0"},
		{
			name:    "ISC",
			content: "Permission to use, copy, modify, and/or distribute\nThe above copyright notice",
			want:    "ISC",
		},
		{
			name:    "MIT",
			content: "Permission is hereby granted, free of charge\nTo deal in the Software without restriction",
			want:    "MIT",
		},
		{
			name:    "BSD three clause",
			content: "Redistribution and use in source and binary forms\nNeither the name",
			want:    "BSD-3-Clause",
		},
		{
			name:    "BSD two clause",
			content: "Redistribution and use in source and binary forms",
			want:    "BSD-2-Clause",
		},
		{name: "unknown", content: "proprietary", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyLicense(tc.content))
		})
	}
}

func TestModuleLicenseRecognizesMixedCaseFilename(t *testing.T) {
	directory := t.TempDir()
	licenseFile := filepath.Join(directory, "License")
	content := "Permission is hereby granted, free of charge\nTo deal in the Software without restriction"
	if err := os.WriteFile(licenseFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	license, err := moduleLicense(directory)

	assert.NoError(t, err)
	assert.Equal(t, "MIT", license)
}
