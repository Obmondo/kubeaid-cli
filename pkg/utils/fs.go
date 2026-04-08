// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Creates a temp dir inside /tmp, where KubeAid Bootstrap Script will clone repos.
// Then sets the value of constants.TempDir as the temp dir path.
// If the temp dir already exists, then that gets reused.
func InitTempDir(ctx context.Context) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("path", constants.TempDirectory),
	})

	// Check if a temp dir already exists for KubeAid Bootstrap Script.
	// If yes, then reuse that.
	if info, err := os.Stat(constants.TempDirectory); err == nil && info.IsDir() {
		slog.InfoContext(ctx, "Skipped creating temp dir, since it already exists")
		return
	}

	// Otherwise, create it.
	err := os.MkdirAll(constants.TempDirectory, 0o750)
	assert.AssertErrNil(ctx, err, "Failed creating temp dir")

	slog.InfoContext(ctx, "Created temp dir")
}

// Returns path to the parent dir of the given file.
func GetParentDirPath(filePath string) string {
	return filepath.Dir(filePath)
}

// Creates intermediate directories which don't exist for the given file path.
func CreateIntermediateDirsForFile(ctx context.Context, filePath string) {
	parentDir := filepath.Dir(filePath)

	err := os.MkdirAll(parentDir, 0o750)
	assert.AssertErrNil(ctx, err,
		"Failed creating intermediate directories for file",
		slog.String("path", filePath),
	)
}

// Returns path to the directory where the KubeAid repository is cloned.
func GetKubeAidDir() string {
	return git.GetRepoDir(config.ParsedGeneralConfig.Forks.KubeaidFork.ParsedURL)
}

// Returns path to the directory where the KubeAid Config repository is cloned.
func GetKubeAidConfigDir() string {
	return git.GetRepoDir(config.ParsedGeneralConfig.Forks.KubeaidConfigFork.ParsedURL)
}

// Returns path to the directory containing cluster specific config, in the KubeAid Config dir.
func GetClusterDir() string {
	return path.Join(
		GetKubeAidConfigDir(),
		"k8s",
		config.ParsedGeneralConfig.Forks.KubeaidConfigFork.Directory,
	)
}

// Returns the path to the local temp directory, where contents of the given blob storage bucket
// will be / is downloaded.
func GetDownloadedStorageBucketContentsDir(bucketName string) string {
	return path.Join(constants.TempDirectory, "buckets", bucketName)
}

// Returns canonical version of the given path.
func ToAbsolutePath(ctx context.Context, path string) string {
	// When the path starts with "~/", we need to expand "~" to the user's home directory path.
	// REFER : https://www.gnu.org/software/bash/manual/html_node/Tilde-Expansion.html.
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		assert.AssertErrNil(ctx, err, "Failed getting home directory")

		path = homeDir + path[1:]
		return path
	}

	absolutePath, err := filepath.Abs(path)
	assert.AssertErrNil(ctx, err, "Failed canonicalizing given path", slog.String("path", path))

	return absolutePath
}

// Moves the source file to the destination file.
//
// But unlike os.Rename( ), it doesn't error out when the source and destination files are present
// on different drives.
func MustMoveFile(ctx context.Context, sourceFilePath, destinationFilePath string) {
	sourceFile, err := os.Open(sourceFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed opening source file",
		slog.String("path", sourceFilePath),
	)
	defer sourceFile.Close()

	destinationFile, err := os.OpenFile(
		destinationFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600,
	)
	assert.AssertErrNil(ctx, err,
		"Failed opening destination file",
		slog.String("path", destinationFilePath),
	)
	defer destinationFile.Close()

	// Copy contents of the source file to the destination file.
	_, err = io.Copy(destinationFile, sourceFile)
	assert.AssertErrNil(ctx, err,
		"Failed copying contents of source file to destination file",
		slog.String("source", sourceFilePath),
		slog.String("destination", destinationFilePath),
	)

	// Delete the source file.
	err = os.Remove(sourceFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed removing source file",
		slog.String("path", sourceFilePath),
	)
}
