package utils

import (
	"log/slog"
	"os"
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
