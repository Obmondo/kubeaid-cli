package utils

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns value of the given environment variable.
// Panics if the environment variable isn't found.
func MustGetEnv(name string) string {
	value, found := os.LookupEnv(name)
	if !found || len(value) == 0 {
		slog.Error("Env not found", slog.String("name", name))
		os.Exit(1)
	}

	return value
}

// Sets value of the given environment variable.
// Panics on error.
func MustSetEnv(name, value string) {
	err := os.Setenv(name, value)
	assert.AssertErrNil(context.Background(), err,
		"Failed setting environment variable",
		slog.String("name", name),
	)
}

func WithRetry(delay time.Duration, attempts uint8, fn func() error) error {
	var err error

	for i := range attempts {
		err = fn()
		if err == nil {
			return nil
		}

		if i < (attempts - 1) { // We don't need to sleep after the last attempt.
			time.Sleep(delay)
		}
	}

	return err
}
