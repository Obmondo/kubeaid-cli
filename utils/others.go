package utils

import (
	"context"
	"log/slog"
	"os"
	"os/exec"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
)

// Returns value of the given environment variable.
// Panics if the environment variable isn't found.
func GetEnv(name string) string {
	value, found := os.LookupEnv(name)
	if !found || len(value) == 0 {
		slog.Error("Env not found", slog.String("name", name))
		os.Exit(1)
	}

	return value
}

func executeCommand(command string, panicOnExecutionFailure bool) (string, error) {
	slog.Info("Executing command", slog.String("command", command))

	output, err := exec.Command("bash", "-c", command).CombinedOutput()
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
