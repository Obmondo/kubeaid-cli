// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package version

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// These variables are set at build time via -ldflags.
// When not set (e.g. plain 'go build' / 'go run'), resolveBuildInfo fills them from the VCS
// build info Go embeds into the binary.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

var VersionCommand = &cobra.Command{
	Use: "version",

	Short: "Print the kubeaid-cli version",

	Run: func(cmd *cobra.Command, args []string) {
		resolveBuildInfo()
		fmt.Printf("version: %s\ncommit:  %s\nbuilt:   %s\n", Version, Commit, Date)
	},
}

// resolveBuildInfo fills Version / Commit / Date from the VCS build info Go embeds when
// building inside a git checkout, for the variables -ldflags didn't inject.
func resolveBuildInfo() {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if (Version == "dev") &&
		(len(buildInfo.Main.Version) > 0) && (buildInfo.Main.Version != "(devel)") {
		Version = buildInfo.Main.Version
	}

	var revision, modified, vcsTime string
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		case "vcs.time":
			vcsTime = setting.Value
		}
	}

	if (Commit == "unknown") && (len(revision) > 0) {
		Commit = revision[:min(7, len(revision))]
		if modified == "true" {
			Commit += "-dirty"
		}
	}

	if (Date == "unknown") && (len(vcsTime) > 0) {
		Date = vcsTime
	}
}
