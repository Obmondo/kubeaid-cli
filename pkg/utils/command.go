package utils

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

func executeCommand(command string, panicOnExecutionFailure bool) (string, error) {
	slog.Info("Executing command", slog.String("command", sensorCredentials(command)))

	output, err := exec.Command("bash", "-c", command).CombinedOutput()
	sensoredOutput := sensorCredentials(string(output))

	if panicOnExecutionFailure {
		assert.AssertErrNil(context.Background(), err,
			"Command execution failed",
			slog.String("output", sensoredOutput),
		)
	}

	// Print out command execution output (if any).
	if len(output) > 0 {
		slog.Debug("Command executed", slog.String("output", sensoredOutput))
	}

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

// Before logging out the command / command execution result, we want to sensor out any contained
// credentials.
func sensorCredentials(input string) string {
	toBeSensoredList := []string{}

	// Determine credentials which need to be sensored.
	switch globals.CloudProviderName {
	case constants.CloudProviderAzure:
		azureSecretsConfig := config.ParsedSecretsConfig.Azure

		toBeSensoredList = append(toBeSensoredList,
			azureSecretsConfig.ClientID,
			azureSecretsConfig.ClientSecret,
		)
	}

	// Sensor each of those credentials.
	for _, toBeSensored := range toBeSensoredList {
		input = strings.ReplaceAll(input, toBeSensored, "**********")
	}

	return input
}
