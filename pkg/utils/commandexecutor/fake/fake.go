// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package fake

import (
	"context"
	"fmt"
)

type Response struct {
	Output string
	Err    error
}

// Executor is a test double for commandexecutor.CommandExecutor.
// It records every command and returns configured responses in order.
type Executor struct {
	Responses []Response
	Commands  []string
}

func NewExecutor(outputs ...string) *Executor {
	responses := make([]Response, len(outputs))
	for i, output := range outputs {
		responses[i] = Response{Output: output}
	}
	return &Executor{Responses: responses}
}

func (e *Executor) Execute(_ context.Context, command string) (string, error) {
	e.Commands = append(e.Commands, command)

	responseIndex := len(e.Commands) - 1
	if responseIndex >= len(e.Responses) {
		return "", fmt.Errorf("fake command executor: unexpected command %q", command)
	}

	response := e.Responses[responseIndex]
	return response.Output, response.Err
}

func (e *Executor) MustExecute(ctx context.Context, command string) string {
	output, err := e.Execute(ctx, command)
	if err != nil {
		panic(err)
	}
	return output
}
