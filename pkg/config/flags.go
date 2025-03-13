package config

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var ConfigFilePath string

func RegisterConfigFilePathFlag(command *cobra.Command) {
	command.PersistentFlags().StringVar(
		&ConfigFilePath,
		constants.FlagNameConfig,
		constants.OutputPathGeneratedConfig,
		"Path to the KubeAid Bootstrap Script config file",
	)
}

var AWSAccessKeyID,
	AWSSecretAccessKey,
	AWSSessionToken string

func RegisterAWSCredentialsFlags(command *cobra.Command) {
	flagSet := pflag.NewFlagSet("aws-credentials", pflag.ExitOnError)

	flagSet.StringVar(&AWSAccessKeyID, constants.FlagNameAWSAccessKeyID, "", "AWS access key ID")
	flagSet.StringVar(&AWSSecretAccessKey, constants.FlagNameAWSSecretAccessKey, "", "AWS secret access key")
	flagSet.StringVar(&AWSSessionToken, constants.FlagNameAWSSessionToken, "", "AWS session token (optional)")

	flagSet.VisitAll(bindFlagToEnv)

	command.Flags().AddFlagSet(flagSet)
}

var HetznerAPIToken,
	HetznerRobotUsername,
	HetznerRobotPassword string

func RegisterHetznerCredentialsFlags(command *cobra.Command) {
	flagSet := pflag.NewFlagSet("hetzner-credentials", pflag.ExitOnError)

	flagSet.StringVar(&HetznerAPIToken, constants.FlagNameHetznerAPIToken, "", "Hetzner API token")
	flagSet.StringVar(&HetznerRobotUsername, constants.FlagNameHetznerRobotUsername, "", "Hetzner robot user")
	flagSet.StringVar(&HetznerRobotPassword, constants.FlagNameHetznerRobotPassword, "", "Hetzner robot password")

	flagSet.VisitAll(bindFlagToEnv)

	command.Flags().AddFlagSet(flagSet)
}

func RegisterAzureCredentialsFlags(command *cobra.Command) {
	flagSet := pflag.NewFlagSet("azure-credentials", pflag.ExitOnError)

	flagSet.VisitAll(bindFlagToEnv)

	command.Flags().AddFlagSet(flagSet)
}

// Usage : flagSet.VisitAll(getFlagOrEnvValue)
//
// If a flag isn't set, then we try to get its value from the corresponding environment variable.
//
// Let's say, if the flag is `--aws-access-key-id` and it's not set, then we'll try to get the
// value of the AWS_ACCESS_KEY_ID environment variable.
//
// NOTE : Doesn't panic, if both the flag and environment variable aren't set and there's no
// default flag value.
func bindFlagToEnv(flag *pflag.Flag) {
	if len(flag.Value.String()) > 0 {
		return
	}

	correspondingEnvName := strings.ReplaceAll(strings.ToUpper(flag.Name), "-", "_")

	envValue, envFound := os.LookupEnv(correspondingEnvName)
	if envFound {
		err := flag.Value.Set(envValue)
		assert.AssertErrNil(context.Background(), err, "Failed setting flag value from environment variable")

		flag.Changed = true

		slog.Debug("Flag value picked up from environment variable", slog.String("flag", flag.Name), slog.String("env", correspondingEnvName))
		return
	}
}
