package utils

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/go-cmd/cmd"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Executes the given command.
// The command output is streamed to the standard output.
func ExecuteCommand(command string) (stdOutOutput string, err error) {
	slog.Info("Executing command", slog.String("command", sensorCredentials(command)))

	commandExecutionOptions := cmd.Options{
		Streaming: true,
	}
	commandExecutor := cmd.NewCmdOptions(commandExecutionOptions,
		"sh", "-c", command,
	)

	// Execute the command,
	// while streaming the stdout contents to the user.
	for !commandExecutor.Status().Complete {
		select {
		case output := <-commandExecutor.Stdout:
			println(output)

			// Keep aggregating the stdout contents in stdOutOutput.
			// We need to return the aggregated result to the invoker.
			stdOutOutput += output

		case output := <-commandExecutor.Stderr:
			// Error occurred, while execution some portion of the command.
			// We'll not execute the remaining portion of the command,
			// but just return the aggregated stdout contents and the error that occurred.
			if len(output) > 0 {
				return stdOutOutput, errors.New(output)
			}

		case <-commandExecutor.Start():
		}
	}
	return stdOutOutput, nil
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
