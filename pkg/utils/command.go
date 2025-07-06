package utils

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/go-cmd/cmd"
)

// Executes the given command.
// The command output is streamed to the standard output.
func ExecuteCommand(command string) (output string, err error) {
	slog.Info("Executing command", slog.String("command", sensorCredentials(command)))

	commandExecutionOptions := cmd.Options{
		CombinedOutput: true,
		Streaming:      true,
	}
	commandExecutor := cmd.NewCmdOptions(commandExecutionOptions,
		"bash", "-c", command,
	)

	// Stream the command execution output to the standard output.
	for !commandExecutor.Status().Complete {
		select {
		case outputLine := <-commandExecutor.Stdout:
			println(outputLine)

		case outputLine := <-commandExecutor.Stderr:
			println(outputLine)

		case <-commandExecutor.Start():
		}
	}

	output = strings.Join(commandExecutor.Status().Stdout, "")
	err = commandExecutor.Status().Error
	return
}

// Executes the given command. Panics if the command execution fails.
func ExecuteCommandOrDie(command string) string {
	output, err := ExecuteCommand(command)
	assert.AssertErrNil(context.Background(), err, "Command execution failed")

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
