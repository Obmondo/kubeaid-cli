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
	command.MarkPersistentFlagRequired(constants.FlagNameConfig)
}

var AWSAccessKey,
	AWSSecretKey,
	AWSSessionToken,
	AWSRegion string

func RegisterAWSCredentialsFlags(command *cobra.Command) {
	flagSet := pflag.NewFlagSet("aws-credentials", pflag.ExitOnError)

	flagSet.StringVar(&AWSAccessKey, constants.FlagNameAWSAccessKey, "", "AWS access key ID")
	cobra.MarkFlagRequired(flagSet, constants.FlagNameAWSAccessKey)

	flagSet.StringVar(&AWSSecretKey, constants.FlagNameAWSSecretKey, "", "AWS secret access key")
	cobra.MarkFlagRequired(flagSet, constants.FlagNameAWSSecretKey)

	flagSet.StringVar(&AWSSessionToken, constants.FlagNameAWSSessionToken, "", "AWS session token (optional)")

	flagSet.StringVar(&AWSRegion, constants.FlagNameAWSRegion, "", "AWS region")
	cobra.MarkFlagRequired(flagSet, constants.FlagNameAWSRegion)

	flagSet.VisitAll(bindFlagToEnv)

	command.Flags().AddFlagSet(flagSet)
}

var HetznerAPIToken,
	HetznerRobotUsername,
	HetznerRobotPassword string

func RegisterHetznerCredentialsFlags(command *cobra.Command) {
	flagSet := pflag.NewFlagSet("aws-credentials", pflag.ExitOnError)

	flagSet.StringVar(&HetznerAPIToken, constants.FlagNameHetznerAPIToken, "", "Hetzner API token")
	command.MarkFlagRequired(constants.FlagNameHetznerAPIToken)

	flagSet.StringVar(&HetznerRobotUsername, constants.FlagNameHetznerRobotUsername, "", "Hetzner robot user")
	command.MarkFlagRequired(constants.FlagNameHetznerRobotUsername)

	flagSet.StringVar(&HetznerRobotPassword, constants.FlagNameHetznerRobotPassword, "", "Hetzner robot password")
	command.MarkFlagRequired(constants.FlagNameHetznerRobotPassword)

	flagSet.VisitAll(bindFlagToEnv)

	command.Flags().AddFlagSet(flagSet)
}

// Usage : flagSet.VisitAll(getFlagOrEnvValue)
//
// If a flag isn't set, then we try to get its value from the corresponding environment variable.
//
// Let's say, if the flag is `--aws-region` and it's not set, then we'll try to get the value of
// the AWS_REGION environment variable.
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
