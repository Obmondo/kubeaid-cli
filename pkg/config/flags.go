package config

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var ConfigsDirectory string

func RegisterConfigsDirectoryFlag(command *cobra.Command) {
	command.PersistentFlags().StringVar(
		&ConfigsDirectory,
		constants.FlagNameConfigsDirectoy,
		constants.OutputPathGeneratedConfigsDirectory,
		"Path to the directory containing KubeAid Bootstrap Script general and secrets config files",
	)
}
