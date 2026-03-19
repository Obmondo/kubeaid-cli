// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package commandexecutor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-cmd/cmd"
	"golang.org/x/sync/errgroup"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type LocalCommandExecutor struct{}

func NewLocalCommandExecutor() CommandExecutor {
	return &LocalCommandExecutor{}
}

func (l *LocalCommandExecutor) Execute(ctx context.Context, command string) (string, error) {
	slog.DebugContext(ctx, "Executing command", slog.String("command", command))

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

	waitGroup, _ := errgroup.WithContext(ctx)

	waitGroup.Go(func() error {
		for line := range commandExecutor.Stdout {
			fmt.Println(line)

			stdoutOutputBuilder.WriteString(line)
		}
		return nil
	})

	waitGroup.Go(func() error {
		for line := range commandExecutor.Stderr {
			fmt.Println(line)

			stderrOutputBuilder.WriteString(line)
		}
		return nil
	})

	commandExecutionStatus := <-commandExecutionStatusChan
	_ = waitGroup.Wait()

	// Command execution finished.

	stdoutOutput := stdoutOutputBuilder.String()
	stderrOutput := stderrOutputBuilder.String()

	if commandExecutionStatus.Error != nil {
		err := fmt.Errorf("%s: %w", stderrOutput, commandExecutionStatus.Error)
		return stdoutOutput, err
	}

	return stdoutOutput, nil
}

func (l *LocalCommandExecutor) MustExecute(ctx context.Context, command string) string {
	output, err := l.Execute(ctx, command)
	assert.AssertErrNil(ctx, err, "Command execution failed")

	return output
}
