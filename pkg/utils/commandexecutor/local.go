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

type LocalCommandExecutor struct {
	// Stream the command execution output to stdout.
	streamOutput bool
}

func NewLocalCommandExecutor(streamOutput bool) CommandExecutor {
	return &LocalCommandExecutor{streamOutput}
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
			if l.streamOutput {
				fmt.Println(line)
			}

			stdoutOutputBuilder.WriteString(line)
		}
		return nil
	})

	waitGroup.Go(func() error {
		for line := range commandExecutor.Stderr {
			if l.streamOutput {
				fmt.Println(line)
			}

			stderrOutputBuilder.WriteString(line)
		}
		return nil
	})

	commandExecutionStatus := <-commandExecutionStatusChan
	_ = waitGroup.Wait()

	// Command execution finished.

	stdoutOutput := stdoutOutputBuilder.String()
	stderrOutput := stderrOutputBuilder.String()

	switch commandExecutionStatus.Exit {
	case 0:
		slog.DebugContext(ctx, "Command executed successfully. Output : \n"+stdoutOutput)

		return stdoutOutput, nil

	default:
		// Two error sources to fold together:
		//   - commandExecutionStatus.Error: only set when go-cmd
		//     itself failed to run the process (e.g. exec.Lookup).
		//     Often nil for "ran fine, exited non-zero" — formatting
		//     it via %w in that case yields the unhelpful
		//     "%!w(<nil>)" the operator sees.
		//   - stderrOutput: what bash/the command actually said.
		// Build a message that's useful regardless of which side has
		// the info, plus the exit code so the operator can look up
		// the failing command's exit semantics.
		slog.DebugContext(ctx, "Command execution failed. Output : \n"+stdoutOutput,
			slog.String("stderr", stderrOutput),
			slog.Int("exit-code", commandExecutionStatus.Exit),
		)

		stderrTrimmed := strings.TrimSpace(stderrOutput)
		if stderrTrimmed == "" {
			stderrTrimmed = "(no stderr output)"
		}
		if commandExecutionStatus.Error != nil {
			return stdoutOutput, fmt.Errorf("command exited %d: %s: %w",
				commandExecutionStatus.Exit, stderrTrimmed, commandExecutionStatus.Error)
		}
		return stdoutOutput, fmt.Errorf("command exited %d: %s",
			commandExecutionStatus.Exit, stderrTrimmed)
	}
}

func (l *LocalCommandExecutor) MustExecute(ctx context.Context, command string) string {
	output, err := l.Execute(ctx, command)
	assert.AssertErrNil(ctx, err, "Command execution failed",
		slog.String("command", command),
	)

	return output
}
