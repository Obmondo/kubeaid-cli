package utils

import (
	"bytes"
	"embed"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/guilherme-santos03/kubeaid-bootstrap-script-guilherme/constants"
)

func SetEnvs() {
	os.Setenv("KUBECONFIG", constants.OutputPathManagementClusterKubeconfig)

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

	parsedTemplate, err := template.New(fileName).Parse(string(contentsAsBytes))
	if err != nil {
		log.Fatalf("Failed parsing template %s : %v", fileName, err)
	}

	var executedTemplate bytes.Buffer
	if err = parsedTemplate.Execute(&executedTemplate, values); err != nil {
		log.Fatalf("Failed executing template %s : %v", fileName, err)
	}
	return executedTemplate.Bytes()
}

func ExecuteCommand(command string, panicOnExecutionFailure bool) string {
	cmd := exec.Command("bash", "-c", command)

	slog.Info("Executing command", slog.String("command", cmd.String()))
	output, err := cmd.CombinedOutput()
	if err != nil && panicOnExecutionFailure {
		log.Fatalf("Command execution failed : %s", string(output))
	}
	slog.Debug("Command executed", slog.String("output", string(output)))
	return string(output)
}

// Executes the given command. Panics if the command execution fails.
func ExecuteCommandOrDie(command string) string {
	return ExecuteCommand(command, true)
}

func Unreachable() { panic("unreachable") }

// Creates intermediate directories which don't exist for the given file path.
func CreateIntermediateDirectories(filePath string) {
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
		log.Fatalf("Failed creating intermediate directories for file path %s : %v", filePath, err)
	}
}
