// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package commandexecutor

import (
	"context"

	"k8c.io/kubeone/pkg/executor"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type SSHCommandExecutor struct {
	connection executor.Interface
}

func NewSSHCommandExecutor(connection executor.Interface) CommandExecutor {
	return &SSHCommandExecutor{connection}
}

func (s *SSHCommandExecutor) Execute(ctx context.Context, command string) (string, error) {
	stdout, _, _, err := s.connection.Exec(command)
	return stdout, err
}

func (s *SSHCommandExecutor) MustExecute(ctx context.Context, command string) string {
	output, err := s.Execute(ctx, command)
	assert.AssertErrNil(ctx, err, "Command execution failed")

	return output
}
