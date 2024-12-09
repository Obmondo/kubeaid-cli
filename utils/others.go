package utils

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
)

// Creates a temp dir inside /tmp, where KubeAid Bootstrap Script will clone repos.
// Then sets the value of constants.TempDir as the temp dir path.
// If the temp dir already exists, then that gets reused.
func InitTempDir() {
	namePrefix := "kubeaid-bootstrap-script-"

	// Check if a temp dir already exists for KubeAid Bootstrap Script.
	// If yes, then reuse that.
	filesAndFolders, err := os.ReadDir("/tmp")
	if err != nil {
		slog.Error("Failed listing files and folders in /tmp", slog.Any("error", err))
		os.Exit(1)
	}
	for _, item := range filesAndFolders {
		if item.IsDir() && strings.HasPrefix(item.Name(), namePrefix) {
			path := "/tmp/" + item.Name()
			slog.Info("Skipped creating temp dir, since it already exists", slog.String("path", path))

			constants.TempDir = path

			return
		}
	}

	// Otherwise, create it.

	dirName := fmt.Sprintf("%s%d", namePrefix, time.Now().Unix())

	path, err := os.MkdirTemp("/tmp", dirName)
	assert.AssertErrNil(context.Background(), err, "Failed creating temp dir", slog.String("path", path))

	slog.Info("Created temp dir", slog.String("path", path))

	constants.TempDir = path
}

// Returns value of the given environment variable.
// Panics if the environment variable isn't found.
func GetEnv(name string) string {
	value, found := os.LookupEnv(name)
	if !found {
		slog.Error("Env not found", slog.String("name", name))
		os.Exit(1)
	}

	return value
}

// Returns path to the parent dir of the given file.
func GetParentDirPath(filePath string) string {
	splitPosition := strings.LastIndex(filePath, "/")
	if splitPosition == -1 {
		return ""
	}
	return filePath[:splitPosition]
}

func executeCommand(command string, panicOnExecutionFailure bool) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	slog.Info("Executing command", slog.String("command", cmd.String()))

	output, err := cmd.CombinedOutput()
	if panicOnExecutionFailure {
		assert.AssertErrNil(context.Background(), err, "Command execution failed", slog.String("output", string(output)))
	}

	slog.Debug("Command executed", slog.String("output", string(output)))
	return string(output), err
}

// Executes the given command. Doesn't panic and returns error (if occurred).
func ExecuteCommand(command string) (string, error) {
	return executeCommand(command, false)
}

// Executes the given command. Panics if the command execution fails.
func ExecuteCommandOrDie(command string) string {
	output, _ := executeCommand(command, true)
	return output
}

// Creates intermediate directories which don't exist for the given file path.
func CreateIntermediateDirsForFile(ctx context.Context, filePath string) {
	parentDir := filepath.Dir(filePath)

	err := os.MkdirAll(parentDir, os.ModePerm)
	assert.AssertErrNil(ctx, err, "Failed creating intermediate directories for file", slog.String("path", filePath))
}

func GetClusterDir() string {
	repoDir := path.Join(constants.TempDir, "kubeaid-config")

	clusterDir := path.Join(repoDir, "k8s", config.ParsedConfig.Cluster.Name)
	return clusterDir
}

// Returns the path to the local temp directory, where contents of the given blob storage bucket
// will be / is downloaded.
func GetDirPathForDownloadedStorageBucketContents(bucketName string) string {
	return path.Join(constants.TempDir, "buckets", bucketName)
}
