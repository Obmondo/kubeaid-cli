// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"fmt"
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
func ExecuteCommand(command string) (string, error) {
	slog.Info("Executing command", slog.String("command", sensorCredentials(command)))

	commandExecutionOptions := cmd.Options{
		Streaming: true,
	}
	commandExecutor := cmd.NewCmdOptions(commandExecutionOptions,
		"bash", "-e", "-c", command,
	)

	var (
		stdoutOutputBuilder = strings.Builder{}
		stderrOutputBuilder = strings.Builder{}
	)

	// Execute the command,
	// while streaming the stdout contents to the user.
	commandExecutionStatusChan := commandExecutor.Start()

	go func() {
		for line := range commandExecutor.Stdout {
			fmt.Println(line)

			stdoutOutputBuilder.WriteString(line)
		}
	}()

	go func() {
		for line := range commandExecutor.Stderr {
			fmt.Println(line)

			stderrOutputBuilder.WriteString(line)
		}
	}()

	commandExecutionStatus := <-commandExecutionStatusChan

	// Command execution finished.
	stdoutOutput := stdoutOutputBuilder.String()
	stderrOutput := stderrOutputBuilder.String()

	if commandExecutionStatus.Error != nil {
		err := fmt.Errorf("%s: %w", stderrOutput, commandExecutionStatus.Error)
		return stdoutOutput, err
	}

	return stdoutOutput, nil
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
