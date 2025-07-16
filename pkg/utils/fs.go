package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Creates a temp dir inside /tmp, where KubeAid Bootstrap Script will clone repos.
// Then sets the value of constants.TempDir as the temp dir path.
// If the temp dir already exists, then that gets reused.
func InitTempDir(ctx context.Context) {
	namePrefix := "kubeaid-bootstrap-script-"

	// Check if a temp dir already exists for KubeAid Bootstrap Script.
	// If yes, then reuse that.
	filesAndFolders, err := os.ReadDir("/tmp")
	assert.AssertErrNil(ctx, err, "Failed listing files and folders in /tmp")
	for _, item := range filesAndFolders {
		if item.IsDir() && strings.HasPrefix(item.Name(), namePrefix) {
			path := "/tmp/" + item.Name()
			slog.InfoContext(ctx,
				"Skipped creating temp dir, since it already exists",
				slog.String("path", path),
			)

			globals.TempDir = path

			return
		}
	}

	// Otherwise, create it.

	dirName := fmt.Sprintf("%s%d", namePrefix, time.Now().Unix())

	path, err := os.MkdirTemp("/tmp", dirName)
	assert.AssertErrNil(ctx, err,
		"Failed creating temp dir",
		slog.String("path", path),
	)

	slog.InfoContext(ctx, "Created temp dir", slog.String("path", path))

	globals.TempDir = path
}

// Returns path to the parent dir of the given file.
func GetParentDirPath(filePath string) string {
	splitPosition := strings.LastIndex(filePath, "/")
	if splitPosition == -1 {
		return ""
	}
	return filePath[:splitPosition]
}

// Creates intermediate directories which don't exist for the given file path.
func CreateIntermediateDirsForFile(ctx context.Context, filePath string) {
	parentDir := filepath.Dir(filePath)

	err := os.MkdirAll(parentDir, os.ModePerm)
	assert.AssertErrNil(ctx, err,
		"Failed creating intermediate directories for file",
		slog.String("path", filePath),
	)
}

// Returns path to the directory (in temp directory), where the KubeAid repo is / will be cloned.
func GetKubeAidDir() string {
	return path.Join(globals.TempDir, "KubeAid")
}

// Returns path to the directory (in temp directory), where the customer's KubeAid Config repo
// is / will be cloned.
func GetKubeAidConfigDir() string {
	return path.Join(globals.TempDir, "kubeaid-config")
}

// Returns path to the directory containing cluster specific config, in the KubeAid Config dir.
func GetClusterDir() string {
	clusterDir := path.Join(GetKubeAidConfigDir(), "k8s", config.ParsedGeneralConfig.Cluster.Name)
	return clusterDir
}

// Returns the path to the local temp directory, where contents of the given blob storage bucket
// will be / is downloaded.
func GetDownloadedStorageBucketContentsDir(bucketName string) string {
	return path.Join(globals.TempDir, "buckets", bucketName)
}

// Converts the given relative path to an absolute path.
func ToAbsolutePath(ctx context.Context, relativePath string) string {
	currentWorkingDirectory, err := os.Getwd()
	assert.AssertErrNil(ctx, err, "Failed getting current working directory")

	absolutePath, err := url.JoinPath(currentWorkingDirectory, relativePath)
	assert.AssertErrNil(ctx, err,
		"Failed joining current working directory with given relative path",
		slog.String("relative-path", relativePath),
	)

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
		destinationFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644,
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
