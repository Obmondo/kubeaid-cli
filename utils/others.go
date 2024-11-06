package utils

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/go-sprout/sprout/sprigin"
)

func SetEnvs() {
	os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathManagementClusterKubeconfig)

	// Cloud provider specific environment variables.
	switch {
	case constants.ParsedConfig.Cloud.AWS != nil:
		os.Setenv("AWS_REGION", constants.ParsedConfig.Cloud.AWS.Region)
		os.Setenv("AWS_ACCESS_KEY_ID", constants.ParsedConfig.Cloud.AWS.AccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", constants.ParsedConfig.Cloud.AWS.SecretKey)
		os.Setenv("AWS_SESSION_TOKEN", constants.ParsedConfig.Cloud.AWS.SessionToken)

		awsB64EncodedCredentials := strings.TrimSpace(
			strings.Split(
				ExecuteCommandOrDie("clusterawsadm bootstrap credentials encode-as-profile"),
				"WARNING: `encode-as-profile` should only be used for bootstrapping.",
			)[1],
		)
		os.Setenv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)

	default:
		Unreachable()
	}
}

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
			constants.TempDir = fmt.Sprintf("/tmp/%s", item.Name())
			return
		}
	}

	// Otherwise, create it.

	name := fmt.Sprintf("%s%d", namePrefix, time.Now().Unix())
	path, err := os.MkdirTemp("/tmp", name)
	if err != nil {
		log.Fatalf("Failed creating temp dir : %v", err)
	}
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

func GetParentDirPath(filePath string) string {
	splitPosition := strings.LastIndex(filePath, "/")
	if splitPosition == -1 {
		return ""
	}
	return filePath[:splitPosition]
}

func ParseAndExecuteTemplate(embeddedFS *embed.FS, fileName string, values any) []byte {
	contentsAsBytes, err := embeddedFS.ReadFile(fileName)
	if err != nil {
		log.Fatalf("Failed getting template %s from embedded file-system : %v", fileName, err)
	}

	parsedTemplate, err := template.New(fileName).Funcs(sprigin.FuncMap()).Parse(string(contentsAsBytes))
	if err != nil {
		log.Fatalf("Failed parsing template %s : %v", fileName, err)
	}

	var executedTemplate bytes.Buffer
	if err = parsedTemplate.Execute(&executedTemplate, values); err != nil {
		log.Fatalf("Failed executing template %s : %v", fileName, err)
	}
	return executedTemplate.Bytes()
}

func executeCommand(command string, panicOnExecutionFailure bool) (string, error) {
	cmd := exec.Command("bash", "-c", command)

	slog.Info("Executing command", slog.String("command", cmd.String()))
	output, err := cmd.CombinedOutput()
	if err != nil && panicOnExecutionFailure {
		log.Fatalf("Command execution failed : %s\n %v", string(output), err)
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

func Unreachable() { panic("unreachable") }

// Creates intermediate directories which don't exist for the given file path.
func CreateIntermediateDirectories(filePath string) {
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
		log.Fatalf("Failed creating intermediate directories for file path %s : %v", filePath, err)
	}
}
