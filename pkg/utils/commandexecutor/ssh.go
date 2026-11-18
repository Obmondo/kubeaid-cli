// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package commandexecutor

import (
	"context"

	"k8c.io/kubeone/pkg/executor"
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
