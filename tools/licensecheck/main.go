// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var approvedLicenses = map[string]struct{}{
	"Apache-2.0":   {},
	"BSD-2-Clause": {},
	"BSD-3-Clause": {},
	"ISC":          {},
	"MIT":          {},
	"MPL-2.0":      {},
}

type goPackage struct {
	Module *goModule
}

type goModule struct {
	Path    string
	Version string
	Dir     string
	Main    bool
}

type dependency struct {
	Module  goModule
	License string
}

func main() {
	report := flag.Bool("report", false, "print a CSV inventory")
	check := flag.Bool("check", false, "fail for an unapproved or unrecognized license")
	flag.Parse()

	if *report == *check {
		fmt.Fprintln(os.Stderr, "exactly one of --report or --check is required")
		os.Exit(2)
	}

	dependencies, err := runtimeDependencies()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *report {
		writeReport(os.Stdout, dependencies)
		return
	}

	if err := checkLicenses(dependencies); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runtimeDependencies() ([]dependency, error) {
	command := exec.Command("go", "list", "-deps", "-json", "./cmd/kubeaid-cli")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("listing runtime dependencies: %w", err)
	}

	decoder := json.NewDecoder(&output)
	modules := make(map[string]goModule)
	for {
		var pkg goPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding Go package metadata: %w", err)
		}
		if pkg.Module != nil && pkg.Module.Dir != "" {
			modules[pkg.Module.Dir] = *pkg.Module
		}
	}

	dependencies := make([]dependency, 0, len(modules))
	for _, module := range modules {
		license, err := moduleLicense(module.Dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", module.Path, err)
		}
		dependencies = append(dependencies, dependency{Module: module, License: license})
	}
	sort.Slice(dependencies, func(i, j int) bool {
		return dependencies[i].Module.Path < dependencies[j].Module.Path
	})
	return dependencies, nil
}

func moduleLicense(directory string) (string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", fmt.Errorf("listing license files: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !isLicenseFile(entry.Name()) {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(directory, file))
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", file, err)
		}
		if license := classifyLicense(string(content)); license != "" {
			return license, nil
		}
	}
	return "", errors.New("no recognized license file")
}

func isLicenseFile(name string) bool {
	name = strings.ToLower(name)
	return strings.HasPrefix(name, "license") ||
		strings.HasPrefix(name, "copying") ||
		strings.HasPrefix(name, "notice")
}

func classifyLicense(content string) string {
	normalized := strings.Join(strings.Fields(strings.ToLower(content)), " ")
	switch {
	case strings.Contains(normalized, "apache license") &&
		strings.Contains(normalized, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(normalized, "mozilla public license, v. 2.0"):
		return "MPL-2.0"
	case strings.Contains(normalized, "permission to use, copy, modify, and/or distribute") &&
		strings.Contains(normalized, "the above copyright notice"):
		return "ISC"
	case strings.Contains(normalized, "permission is hereby granted, free of charge") &&
		strings.Contains(normalized, "to deal in the software without restriction"):
		return "MIT"
	case strings.Contains(normalized, "redistribution and use in source and binary forms"):
		if strings.Contains(normalized, "neither the name") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	default:
		return ""
	}
}

func checkLicenses(dependencies []dependency) error {
	var rejected []string
	for _, dependency := range dependencies {
		if _, ok := approvedLicenses[dependency.License]; !ok {
			rejected = append(rejected,
				fmt.Sprintf("%s@%s: %s", dependency.Module.Path, dependency.Module.Version, dependency.License),
			)
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	return fmt.Errorf("unapproved dependency licenses:\n%s", strings.Join(rejected, "\n"))
}

func writeReport(writer io.Writer, dependencies []dependency) {
	report := csv.NewWriter(writer)
	_ = report.Write([]string{"module", "version", "license"})
	for _, dependency := range dependencies {
		version := dependency.Module.Version
		if dependency.Module.Main {
			version = "main"
		}
		_ = report.Write([]string{dependency.Module.Path, version, dependency.License})
	}
	report.Flush()
	if err := report.Error(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
