package utils

import (
	"log/slog"
	"os"
	"time"
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

func WithRetry(delay time.Duration, attempts uint8, fn func() error) error {
	var err error = nil

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
